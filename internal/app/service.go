package app

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"mini-proxy/internal/cert"
	"mini-proxy/internal/license"
	"mini-proxy/internal/notify"
	"mini-proxy/internal/proxy"
	"mini-proxy/internal/rules"
	"mini-proxy/internal/sku"
	"mini-proxy/internal/uiauto"
	"mini-proxy/internal/winproxy"
)

type Status struct {
	ProxyRunning      bool   `json:"proxyRunning"`
	Addr              string `json:"addr"`
	RulesPath         string `json:"rulesPath"`
	SystemProxyActive bool   `json:"systemProxyActive"`
	RootCertPath      string `json:"rootCertPath"`
	RootThumbprint    string `json:"rootThumbprint"`
	RootTrusted       bool   `json:"rootTrusted"`
	BaseDir           string `json:"baseDir"`
	LogDir            string `json:"logDir"`
	ProxyStatePath    string `json:"proxyStatePath"`
	Licensed          bool   `json:"licensed"`
	LastError         string `json:"lastError"`
}

type JDAutomationStatus struct {
	Running      bool   `json:"running"`
	CurrentCycle int    `json:"currentCycle"`
	TotalCycles  int    `json:"totalCycles"`
	LastError    string `json:"lastError"`
}

type Service struct {
	mu                sync.Mutex
	paths             Paths
	logger            *log.Logger
	cleanupLogger     func()
	certManager       *cert.Manager
	proxyServer       *proxy.Server
	proxyStarting     bool
	proxyStopping     bool
	skuStore          *sku.Store
	licenseStore      *license.Store
	licenseClient     *license.Client
	licenseServerURL  string
	deviceID          string
	rulesPath         string
	addr              string
	systemProxyActive bool
	lastError         string

	jdAutomationCancel        context.CancelFunc
	jdAutomationDone          chan struct{}
	jdAutomationStarting      bool
	jdAutomationStopRequested bool
	jdAutomationStatus        JDAutomationStatus
}

func NewService() (*Service, error) {
	paths, err := DefaultPaths()
	if err != nil {
		return nil, err
	}
	logger, cleanup, err := NewLogger(paths)
	if err != nil {
		return nil, err
	}
	certManager, err := cert.NewManager(paths.CertDir)
	if err != nil {
		cleanup()
		return nil, err
	}
	skuStore := sku.NewStore()
	if err := loadSKUSnapshot(filepath.Join(paths.LogDir, "intercepts", "sku-latest.json"), skuStore); err != nil {
		logger.Printf("load sku snapshot failed: %v", err)
	}

	licenseStore := license.NewStore(paths.LicenseStatePath)
	deviceID := ensureDeviceID(paths, logger)
	licenseServerURL := loadLicenseServerURL(paths.LicenseServerPath)

	// If a previous run enabled the Windows system proxy and then exited
	// uncleanly (crash / force-kill), the system proxy is left pointing at our
	// now-dead listener, which breaks all internet access. Recover it here.
	recoverDanglingSystemProxy(paths, logger)

	return &Service{
		paths:            paths,
		logger:           logger,
		cleanupLogger:    cleanup,
		certManager:      certManager,
		skuStore:         skuStore,
		licenseStore:     licenseStore,
		licenseClient:    license.NewClient(licenseServerURL),
		licenseServerURL: licenseServerURL,
		deviceID:         deviceID,
		addr:             "127.0.0.1:8899",
		rulesPath:        ResolveRuntimePath("configs/jd.rules.json"),
	}, nil
}

func loadSKUSnapshot(path string, store *sku.Store) error {
	if store == nil {
		return nil
	}
	content, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var snapshot sku.Snapshot
	if err := json.Unmarshal(content, &snapshot); err != nil {
		return err
	}
	store.LoadSnapshot(snapshot)
	return nil
}

func (service *Service) Status() Status {
	service.mu.Lock()
	defer service.mu.Unlock()

	licensed, _ := service.checkLicense()
	return Status{
		ProxyRunning:      service.proxyServer != nil,
		Addr:              service.addr,
		RulesPath:         service.rulesPath,
		SystemProxyActive: service.systemProxyActive,
		RootCertPath:      service.certManager.RootCertPath(),
		RootThumbprint:    service.certManager.Thumbprint(),
		RootTrusted:       service.certManager.IsTrustedRootInstalled(),
		BaseDir:           service.paths.BaseDir,
		LogDir:            service.paths.LogDir,
		ProxyStatePath:    service.paths.ProxyStatePath,
		Licensed:          licensed,
		LastError:         service.lastError,
	}
}

// GetRequestLogs returns the most recent proxied requests handled by the
// currently running proxy server, oldest first. It returns an empty slice
// when the proxy is not running.
func (service *Service) GetRequestLogs() []proxy.RequestLogEntry {
	service.mu.Lock()
	server := service.proxyServer
	service.mu.Unlock()
	if server == nil {
		return []proxy.RequestLogEntry{}
	}
	return server.RecentLogs()
}

// GetSKUList returns the SKU list accumulated from intercepted JD cartview
// responses. The store is owned by the service, so the list persists across
// proxy restarts within a single app session.
func (service *Service) GetSKUList() sku.Snapshot {
	service.mu.Lock()
	store := service.skuStore
	service.mu.Unlock()
	if store == nil {
		return sku.Snapshot{}
	}
	return store.Snapshot()
}

// ResetSKUList clears every accumulated SKU and its change history.
func (service *Service) ResetSKUList() {
	service.mu.Lock()
	store := service.skuStore
	server := service.proxyServer
	snapshotPath := filepath.Join(service.paths.LogDir, "intercepts", "sku-latest.json")
	service.mu.Unlock()
	if server != nil {
		server.ResetSKUList()
		return
	}
	if store != nil {
		store.Reset()
	}
	if err := os.Remove(snapshotPath); err != nil && !os.IsNotExist(err) {
		service.logger.Printf("remove sku snapshot failed: %v", err)
	}
}

// checkLicense is the offline gate used before starting the proxy. It trusts a
// cached signed token (validated against the embedded public key, anchored on
// the server's authoritative time) — no network call in the hot path. A fresh
// online verify is done separately by VerifyLicense (called by the UI on load).
func (service *Service) checkLicense() (bool, error) {
	store := service.licenseStore
	if store == nil {
		return true, nil
	}
	state, err := store.Load()
	if err != nil {
		return false, err
	}
	if license.IsValidCached(state, service.deviceID, time.Now()) {
		return true, nil
	}
	return false, errors.New("尚未授权或授权已失效，请激活授权码")
}

// ActivateLicense binds this device to a license key on the license server,
// verifies the returned signed token client-side, and persists it. The input is
// a plain license key in XXXX-XXXX-XXXX-XXXX form (same as jd-chrome-plugin).
func (service *Service) ActivateLicense(rawKey string) error {
	key := strings.TrimSpace(rawKey)
	if key == "" {
		err := errors.New("请输入授权码")
		service.setLastError(err)
		return err
	}
	state, err := service.licenseClient.Activate(key, service.deviceID, time.Now())
	if err != nil {
		service.setLastError(err)
		return err
	}
	if err := service.licenseStore.Save(state); err != nil {
		service.setLastError(err)
		return err
	}
	service.setLastError(nil)
	service.logger.Printf("license activated key=%s device=%s expires=%s", key, service.deviceID, state.ExpiresAt)
	return nil
}

// VerifyLicense performs an online re-verification (mirrors the extension's
// verifyLicenseOnline): if a key is cached it calls /verify; otherwise it tries
// /auto-unlock by device. On a definitive server rejection the cached state is
// cleared. It returns whether the device is authorized.
func (service *Service) VerifyLicense() (bool, error) {
	store := service.licenseStore
	client := service.licenseClient
	now := time.Now()

	cached, _ := store.Load()
	key := cached.Key

	if key == "" {
		state, err := client.AutoUnlock(service.deviceID, now)
		if err != nil {
			// No key + auto-unlock failed → simply unauthorized (not a hard error).
			return false, nil
		}
		if err := store.Save(state); err != nil {
			service.setLastError(err)
			return false, err
		}
		service.setLastError(nil)
		return true, nil
	}

	state, err := client.Verify(key, service.deviceID, now)
	if err != nil {
		// Distinguish a definitive server rejection from a transient network error.
		switch license.ErrorCode(err) {
		case "revoked", "expired", "device-mismatch", "license-not-found", "key-mismatch":
			_ = store.Clear()
			service.stopProxyAfterLicenseInvalidation()
			service.setLastError(err)
			return false, err
		default:
			// Network/other: keep the cache, report not-authorized-now softly.
			return license.IsValidCached(cached, service.deviceID, now), err
		}
	}
	if err := store.Save(state); err != nil {
		service.setLastError(err)
		return false, err
	}
	service.setLastError(nil)
	return true, nil
}

// DeactivateLicense removes the persisted license state.
func (service *Service) DeactivateLicense() error {
	if service.licenseStore == nil {
		return nil
	}
	if err := service.licenseStore.Clear(); err != nil {
		service.setLastError(err)
		return err
	}
	service.stopProxyAfterLicenseInvalidation()
	service.setLastError(nil)
	return nil
}

func (service *Service) stopProxyAfterLicenseInvalidation() {
	service.mu.Lock()
	running := service.proxyServer != nil || service.systemProxyActive
	service.mu.Unlock()
	if !running {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := service.StopProxy(ctx); err != nil && service.logger != nil {
		service.logger.Printf("stop proxy after license invalidation failed: %v", err)
	}
}

// GetLicenseState returns the persisted signed license state for UI display.
func (service *Service) GetLicenseState() license.State {
	if service.licenseStore == nil {
		return license.State{}
	}
	state, _ := service.licenseStore.Load()
	return state
}

// GetDeviceID returns the hardware-bound device fingerprint.
func (service *Service) GetDeviceID() string {
	return service.deviceID
}

// GetLicenseServerURL returns the configured license server base URL.
func (service *Service) GetLicenseServerURL() string {
	service.mu.Lock()
	defer service.mu.Unlock()
	if service.licenseServerURL == "" {
		return license.DefaultServerURL
	}
	return service.licenseServerURL
}

// SetLicenseServerURL persists a new license server base URL and rebuilds the
// client. An empty value resets to the default.
func (service *Service) SetLicenseServerURL(url string) error {
	url = strings.TrimSpace(url)
	if url == "" {
		url = license.DefaultServerURL
	}
	if err := saveLicenseServerURL(service.paths.LicenseServerPath, url); err != nil {
		service.setLastError(err)
		return err
	}
	service.mu.Lock()
	service.licenseServerURL = url
	service.licenseClient = license.NewClient(url)
	service.mu.Unlock()
	service.setLastError(nil)
	return nil
}

// GetNotifyConfig returns the persisted DingTalk notification + discount config.
// A missing config file yields sensible disabled defaults.
func (service *Service) GetNotifyConfig() (notify.Config, error) {
	config, err := LoadNotifyConfig(service.paths.NotifyConfigPath)
	if err != nil {
		service.setLastError(err)
		return config, err
	}
	return config, nil
}

// SaveNotifyConfig validates and persists the notification config. The running
// proxy picks up the change on its next start (the notifier is created when the
// proxy starts, mirroring how rules are loaded).
func (service *Service) SaveNotifyConfig(config notify.Config) error {
	if _, err := notify.New(config, service.logger); err != nil {
		service.setLastError(err)
		return err
	}
	if err := SaveNotifyConfig(service.paths.NotifyConfigPath, config); err != nil {
		service.setLastError(err)
		return err
	}
	service.setLastError(nil)
	return nil
}

// TestNotify sends a sample DingTalk message using the provided config so users
// can verify the webhook URL, signing secret, and template before saving.
func (service *Service) TestNotify(config notify.Config) error {
	notifier, err := notify.New(config, service.logger)
	if err != nil {
		service.setLastError(err)
		return err
	}
	if err := notifier.SendTest(); err != nil {
		service.setLastError(err)
		return err
	}
	service.setLastError(nil)
	return nil
}

// AutoStartProxy starts the default desktop proxy only when this device has a
// valid cached license. Unlicensed first launches remain on the activation
// screen without changing Windows proxy settings.
func (service *Service) AutoStartProxy() error {
	licensed, _ := service.checkLicense()
	if !licensed {
		return nil
	}
	_, err := service.StartProxy(ServeOptions{
		Addr:              "127.0.0.1:8899",
		RulesPath:         ResolveRuntimePath("configs/jd.rules.json"),
		EnableSystemProxy: true,
		ProxyOverride:     "localhost;127.0.0.1;<local>",
	})
	return err
}

func (service *Service) StartProxy(options ServeOptions) (Status, error) {
	service.mu.Lock()
	if service.proxyStopping {
		service.mu.Unlock()
		return service.Status(), fmt.Errorf("proxy is still stopping")
	}
	if service.proxyServer != nil || service.proxyStarting {
		service.mu.Unlock()
		return service.Status(), fmt.Errorf("proxy is already running")
	}
	service.proxyStarting = true
	service.mu.Unlock()
	defer func() {
		service.mu.Lock()
		service.proxyStarting = false
		service.mu.Unlock()
	}()

	// License gate: block proxy start when unlicensed.
	if licensed, err := service.checkLicense(); !licensed {
		return service.Status(), fmt.Errorf("license required: %w", err)
	}

	if options.Addr == "" {
		options.Addr = "127.0.0.1:8899"
	}
	if options.RulesPath == "" {
		options.RulesPath = "configs/jd.rules.json"
	}
	options.RulesPath = ResolveRuntimePath(options.RulesPath)
	if options.ProxyOverride == "" {
		options.ProxyOverride = "localhost;127.0.0.1;<local>"
	}

	ruleSet, err := rules.Load(options.RulesPath)
	if err != nil {
		service.setLastError(err)
		return service.Status(), err
	}

	if _, err := service.certManager.EnsureTrustedRoot(); err != nil {
		service.setLastError(err)
		return service.Status(), err
	}

	proxyServer := proxy.New(proxy.Config{
		Addr:       options.Addr,
		Rules:      ruleSet,
		Certs:      service.certManager,
		Logger:     service.logger,
		SKUStore:   service.skuStore,
		Notifier:   loadNotifier(service.paths.NotifyConfigPath, service.logger),
		CaptureDir: filepath.Join(service.paths.LogDir, "intercepts"),
	})
	listener, err := net.Listen("tcp", proxyServer.Addr())
	if err != nil {
		service.setLastError(err)
		return service.Status(), err
	}

	if options.EnableSystemProxy {
		previousState, err := winproxy.Read()
		if err != nil {
			_ = listener.Close()
			service.setLastError(err)
			return service.Status(), err
		}
		// Never persist a "previous" state that already points at our own proxy
		// (would happen after an earlier unclean exit). Restoring it would just
		// re-point the system proxy at a dead port, so save a disabled state
		// instead — restoring then cleanly turns the system proxy off.
		if proxyPointsToLocalProxy(previousState.Server, listener.Addr().String()) {
			previousState = winproxy.State{Override: previousState.Override}
		}
		if err := winproxy.SaveState(service.paths.ProxyStatePath, previousState); err != nil {
			_ = listener.Close()
			service.setLastError(err)
			return service.Status(), err
		}
		if err := winproxy.Enable(listener.Addr().String(), options.ProxyOverride); err != nil {
			_ = listener.Close()
			_ = winproxy.Restore(previousState)
			_ = os.Remove(service.paths.ProxyStatePath)
			service.setLastError(err)
			return service.Status(), err
		}
	}

	service.mu.Lock()
	service.proxyServer = proxyServer
	service.addr = listener.Addr().String()
	service.rulesPath = options.RulesPath
	service.systemProxyActive = options.EnableSystemProxy
	service.lastError = ""
	service.mu.Unlock()

	go func() {
		if err := proxyServer.Serve(listener); err != nil {
			service.logger.Printf("proxy stopped with error: %v", err)
			service.handleUnexpectedProxyExit(proxyServer, err)
		}
	}()

	return service.Status(), nil
}

func (service *Service) StopProxy(ctx context.Context) (Status, error) {
	service.mu.Lock()
	if service.proxyStarting {
		service.mu.Unlock()
		return service.Status(), fmt.Errorf("proxy is still starting")
	}
	if service.proxyStopping {
		service.mu.Unlock()
		return service.Status(), fmt.Errorf("proxy is already stopping")
	}
	proxyServer := service.proxyServer
	systemProxyActive := service.systemProxyActive
	service.proxyStopping = proxyServer != nil || systemProxyActive
	service.proxyServer = nil
	service.systemProxyActive = false
	service.mu.Unlock()
	defer func() {
		service.mu.Lock()
		service.proxyStopping = false
		service.mu.Unlock()
	}()

	var stopErr error
	if proxyServer != nil {
		stopErr = proxyServer.Shutdown(ctx)
	}
	if systemProxyActive {
		if state, err := winproxy.LoadState(service.paths.ProxyStatePath); err != nil {
			service.logger.Printf("load previous proxy state failed: %v", err)
			if stopErr == nil {
				stopErr = err
			}
		} else if err := winproxy.Restore(state); err != nil {
			service.logger.Printf("restore previous proxy state failed: %v", err)
			if stopErr == nil {
				stopErr = err
			}
		} else {
			// Clean restore: drop the recovery marker so the next launch does
			// not treat this run as an unclean exit.
			_ = os.Remove(service.paths.ProxyStatePath)
		}
	}
	if stopErr != nil {
		service.setLastError(stopErr)
		return service.Status(), stopErr
	}
	service.setLastError(nil)
	return service.Status(), nil
}

func (service *Service) handleUnexpectedProxyExit(proxyServer *proxy.Server, serveErr error) {
	service.mu.Lock()
	if service.proxyServer != proxyServer {
		service.mu.Unlock()
		return
	}
	systemProxyActive := service.systemProxyActive
	service.proxyStopping = true
	service.proxyServer = nil
	service.systemProxyActive = false
	service.mu.Unlock()

	if systemProxyActive {
		if state, err := winproxy.LoadState(service.paths.ProxyStatePath); err != nil {
			service.logger.Printf("load previous proxy state after server exit failed: %v", err)
		} else if err := winproxy.Restore(state); err != nil {
			service.logger.Printf("restore previous proxy state after server exit failed: %v", err)
		} else {
			_ = os.Remove(service.paths.ProxyStatePath)
		}
	}
	service.mu.Lock()
	service.proxyStopping = false
	service.mu.Unlock()
	service.setLastError(serveErr)
}

func (service *Service) InstallCert() (Status, error) {
	err := service.certManager.InstallTrustedRoot()
	service.setLastError(err)
	return service.Status(), err
}

// EnsureCert installs the root certificate only when it is not already trusted.
func (service *Service) EnsureCert() (Status, error) {
	_, err := service.certManager.EnsureTrustedRoot()
	service.setLastError(err)
	return service.Status(), err
}

func (service *Service) UninstallCert() (Status, error) {
	err := service.certManager.UninstallTrustedRoot()
	service.setLastError(err)
	return service.Status(), err
}

func (service *Service) ReadTextFile(path string) (string, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		service.setLastError(err)
		return "", err
	}
	return string(content), nil
}

func (service *Service) WriteTextFile(path string, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		service.setLastError(err)
		return err
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		service.setLastError(err)
		return err
	}
	service.setLastError(nil)
	return nil
}

func (service *Service) InspectAutomation(path string) (string, error) {
	config, err := uiauto.Load(path)
	if err != nil {
		service.setLastError(err)
		return "", err
	}
	if len(config.Sequences) == 0 {
		err := fmt.Errorf("automation config must include at least one sequence")
		service.setLastError(err)
		return "", err
	}
	output, err := uiauto.Inspect(context.Background(), config.Sequences[0].Window)
	service.setLastError(err)
	return output, err
}

func (service *Service) RunAutomation(path string) error {
	config, err := uiauto.Load(path)
	if err != nil {
		service.setLastError(err)
		return err
	}
	err = uiauto.Run(context.Background(), config, service.logger)
	service.setLastError(err)
	return err
}

// StartJDAutomation launches the WeChat/JD mini-program cart-cycle automation in the
// background (see internal/uiauto.RunCoordCycle). It only ever navigates between the
// cart's "全部" and "服务" tabs; it never confirms orders or submits payments. Only one
// run can be active at a time.
func (service *Service) StartJDAutomation(options uiauto.CoordCycleOptions) (JDAutomationStatus, error) {
	if !service.reserveJDAutomationStart() {
		return service.GetJDAutomationStatus(), fmt.Errorf("JD automation is already running")
	}
	defer service.releaseJDAutomationStart()

	// Pre-check that the target mini-program window is already open, so the user
	// gets an immediate, clear prompt instead of the run failing partway through.
	if err := uiauto.CheckWindowAvailable(options); err != nil {
		service.mu.Lock()
		service.jdAutomationStatus = JDAutomationStatus{Running: false, LastError: err.Error()}
		service.mu.Unlock()
		return service.GetJDAutomationStatus(), err
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	if !service.completeJDAutomationStart(cancel, done, options.RepeatCount) {
		cancel()
		return service.GetJDAutomationStatus(), context.Canceled
	}

	go func() {
		err := uiauto.RunCoordCycle(ctx, options, service.logger, func(cycle int) {
			service.mu.Lock()
			service.jdAutomationStatus.CurrentCycle = cycle
			service.mu.Unlock()
		})

		service.mu.Lock()
		service.jdAutomationCancel = nil
		service.jdAutomationDone = nil
		service.jdAutomationStatus.Running = false
		if err != nil && err != context.Canceled {
			service.jdAutomationStatus.LastError = err.Error()
		} else {
			service.jdAutomationStatus.LastError = ""
		}
		service.mu.Unlock()
		close(done)
	}()

	return service.GetJDAutomationStatus(), nil
}

func (service *Service) reserveJDAutomationStart() bool {
	service.mu.Lock()
	defer service.mu.Unlock()
	if service.jdAutomationStarting || service.jdAutomationCancel != nil {
		return false
	}
	service.jdAutomationStarting = true
	service.jdAutomationStopRequested = false
	return true
}

func (service *Service) completeJDAutomationStart(cancel context.CancelFunc, done chan struct{}, totalCycles int) bool {
	service.mu.Lock()
	defer service.mu.Unlock()
	if service.jdAutomationStopRequested {
		service.jdAutomationStarting = false
		return false
	}
	service.jdAutomationCancel = cancel
	service.jdAutomationDone = done
	service.jdAutomationStarting = false
	service.jdAutomationStatus = JDAutomationStatus{Running: true, TotalCycles: totalCycles}
	return true
}

func (service *Service) releaseJDAutomationStart() {
	service.mu.Lock()
	service.jdAutomationStarting = false
	service.mu.Unlock()
}

// StopJDAutomation cancels a running JD automation cycle, if any. It is safe to call
// even when nothing is running.
func (service *Service) StopJDAutomation() JDAutomationStatus {
	service.mu.Lock()
	if service.jdAutomationStarting {
		service.jdAutomationStopRequested = true
	}
	cancel := service.jdAutomationCancel
	done := service.jdAutomationDone
	service.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if done != nil {
		<-done
	}
	return service.GetJDAutomationStatus()
}

func (service *Service) GetJDAutomationStatus() JDAutomationStatus {
	service.mu.Lock()
	defer service.mu.Unlock()
	return service.jdAutomationStatus
}

func (service *Service) Close(ctx context.Context) error {
	if ctx == nil {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
	}
	service.StopJDAutomation()
	_, err := service.StopProxy(ctx)
	if service.cleanupLogger != nil {
		service.cleanupLogger()
	}
	return err
}

func (service *Service) setLastError(err error) {
	service.mu.Lock()
	defer service.mu.Unlock()
	if err == nil {
		service.lastError = ""
		return
	}
	service.lastError = err.Error()
}

// proxyPointsToLocalProxy reports whether a Windows ProxyServer string refers to
// this app's own loopback proxy (so it must not be persisted as a "previous"
// state, otherwise restoring it would re-point the system proxy at a dead port).
func proxyPointsToLocalProxy(server string, addr string) bool {
	server = strings.TrimSpace(server)
	if server == "" {
		return false
	}
	if addr != "" && strings.Contains(server, addr) {
		return true
	}
	return strings.Contains(server, "127.0.0.1:8899")
}

// recoverDanglingSystemProxy restores the saved previous system-proxy state when
// a state file is left behind by a previous unclean exit (crash / force-kill).
// The state file only exists between a successful StartProxy(EnableSystemProxy)
// and a clean StopProxy; if it is present at startup, the last run did not clean
// up. If the saved state itself points back at our proxy, the system proxy is
// disabled instead so the user's internet is not left broken.
func recoverDanglingSystemProxy(paths Paths, logger *log.Logger) {
	if _, err := os.Stat(paths.ProxyStatePath); err != nil {
		return
	}
	state, err := winproxy.LoadState(paths.ProxyStatePath)
	if err != nil {
		logger.Printf("recover system proxy: load state failed: %v", err)
		current, readErr := winproxy.Read()
		if readErr != nil || !proxyPointsToLocalProxy(current.Server, "") {
			return
		}
		fallback := winproxy.State{Override: current.Override}
		if restoreErr := winproxy.Restore(fallback); restoreErr != nil {
			logger.Printf("recover system proxy: disable local proxy after corrupt state failed: %v", restoreErr)
			return
		}
		_ = os.Remove(paths.ProxyStatePath)
		logger.Printf("disabled dangling local proxy after corrupt recovery state")
		return
	}
	if proxyPointsToLocalProxy(state.Server, "") {
		state = winproxy.State{Override: state.Override}
	}
	if err := winproxy.Restore(state); err != nil {
		logger.Printf("recover system proxy: restore failed: %v", err)
		return
	}
	_ = os.Remove(paths.ProxyStatePath)
	logger.Printf("recovered system proxy after previous unclean exit")
}

// ensureDeviceID returns a stable machine-bound identifier, persisting a UUID
// fallback on disk when the hardware fingerprint is unavailable.
func ensureDeviceID(paths Paths, logger *log.Logger) string {
	id := license.DeviceID()
	if id != "" {
		return id
	}
	data, err := os.ReadFile(paths.DeviceIDPath)
	if err == nil && len(data) > 0 {
		return strings.TrimSpace(string(data))
	}
	fallback := generateUUID()
	if err := os.MkdirAll(filepath.Dir(paths.DeviceIDPath), 0o700); err == nil {
		if err := os.WriteFile(paths.DeviceIDPath, []byte(fallback), 0o600); err != nil {
			logger.Printf("failed to persist device-id fallback: %v", err)
		}
	}
	return fallback
}

// loadLicenseServerURL reads the persisted license server base URL, defaulting
// to license.DefaultServerURL when the file is missing or empty.
func loadLicenseServerURL(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return license.DefaultServerURL
	}
	url := strings.TrimSpace(string(data))
	if url == "" {
		return license.DefaultServerURL
	}
	return url
}

// saveLicenseServerURL persists the license server base URL.
func saveLicenseServerURL(path, url string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(strings.TrimSpace(url)), 0o600)
}

func generateUUID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
