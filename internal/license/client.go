package license

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// Client calls the jd-chrome-plugin license server's public API. It reuses the
// exact endpoints and request/response shapes so the same server can gate this
// app unchanged.
type Client struct {
	baseURL string
	http    *http.Client
}

// NewClient builds a Client for the given base URL (e.g. http://host:8787).
// An empty URL falls back to DefaultServerURL.
func NewClient(baseURL string) *Client {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		baseURL = DefaultServerURL
	}
	return &Client{
		baseURL: baseURL,
		http:    &http.Client{Timeout: 12 * time.Second},
	}
}

// BaseURL returns the configured server base URL.
func (c *Client) BaseURL() string { return c.baseURL }

// serverResponse is the common shape of the server's license endpoints.
type serverResponse struct {
	OK        bool          `json:"ok"`
	Error     string        `json:"error"`
	ExpiresAt string        `json:"expiresAt"`
	Key       string        `json:"key"`
	Payload   *TokenPayload `json:"payload"`
	Signature string        `json:"signature"`
}

// ServerError is a stable machine-readable rejection returned by the license
// server. Callers should inspect Code instead of matching localized strings.
type ServerError struct {
	Code string
}

func (err *ServerError) Error() string { return err.Code }

// ErrorCode extracts a server rejection code, or an empty string for network,
// decoding, and other local errors.
func ErrorCode(err error) string {
	var serverErr *ServerError
	if errors.As(err, &serverErr) {
		return serverErr.Code
	}
	return ""
}

func (c *Client) post(path string, body any) (serverResponse, error) {
	raw, err := json.Marshal(body)
	if err != nil {
		return serverResponse{}, err
	}
	request, err := http.NewRequest(http.MethodPost, c.baseURL+path, bytes.NewReader(raw))
	if err != nil {
		return serverResponse{}, err
	}
	request.Header.Set("Content-Type", "application/json")

	response, err := c.http.Do(request)
	if err != nil {
		return serverResponse{}, fmt.Errorf("网络请求失败：%w", err)
	}
	defer response.Body.Close()

	var parsed serverResponse
	if err := json.NewDecoder(response.Body).Decode(&parsed); err != nil {
		return serverResponse{}, fmt.Errorf("授权服务器返回异常 (HTTP %d)", response.StatusCode)
	}
	return parsed, nil
}

// applyResponse verifies the signed token in a server response and, on success,
// returns the persistable State. It enforces signature + device + key match,
// exactly like the extension's applyLicenseStateFromServer.
func applyResponse(resp serverResponse, key, deviceID string, now time.Time) (State, error) {
	if resp.Payload == nil || resp.Signature == "" {
		return State{}, &ServerError{Code: "invalid-response"}
	}
	if err := VerifySignature(*resp.Payload, resp.Signature); err != nil {
		return State{}, &ServerError{Code: "invalid-signature"}
	}
	if resp.Payload.DeviceID != deviceID {
		return State{}, &ServerError{Code: "device-mismatch"}
	}
	if key != "" && resp.Payload.Key != key {
		return State{}, &ServerError{Code: "key-mismatch"}
	}
	if resp.Payload.Status != "active" {
		return State{}, &ServerError{Code: "revoked"}
	}
	serverTime, serverTimeOK := parseISO(resp.Payload.ServerTime)
	expiresAt, expiresAtOK := parseISO(resp.Payload.ExpiresAt)
	if !serverTimeOK || !expiresAtOK {
		return State{}, &ServerError{Code: "invalid-response"}
	}
	if !expiresAt.After(serverTime) {
		return State{}, &ServerError{Code: "expired"}
	}
	return StateFromServer(*resp.Payload, resp.Signature, now), nil
}

// Activate binds the device to the license key on the server and returns the
// signed, verified State.
func (c *Client) Activate(key, deviceID string, now time.Time) (State, error) {
	resp, err := c.post("/api/license/activate", map[string]string{"key": key, "deviceId": deviceID})
	if err != nil {
		return State{}, err
	}
	if !resp.OK {
		return State{}, serverError(resp.Error)
	}
	return applyResponse(resp, key, deviceID, now)
}

// Verify re-checks an already-bound license and returns a refreshed State.
func (c *Client) Verify(key, deviceID string, now time.Time) (State, error) {
	resp, err := c.post("/api/license/verify", map[string]string{"key": key, "deviceId": deviceID})
	if err != nil {
		return State{}, err
	}
	if !resp.OK {
		return State{}, serverError(resp.Error)
	}
	return applyResponse(resp, key, deviceID, now)
}

// AutoUnlock asks the server whether this device already has an active license
// bound (no key needed) and returns the signed State when it does.
func (c *Client) AutoUnlock(deviceID string, now time.Time) (State, error) {
	resp, err := c.post("/api/license/auto-unlock", map[string]string{"deviceId": deviceID})
	if err != nil {
		return State{}, err
	}
	if !resp.OK {
		return State{}, serverError(resp.Error)
	}
	return applyResponse(resp, resp.Key, deviceID, now)
}

// serverError maps a server error code to a non-nil error, defaulting sensibly.
func serverError(code string) error {
	if code == "" {
		code = "unauthorized"
	}
	return &ServerError{Code: code}
}
