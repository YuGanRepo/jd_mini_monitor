package rules

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

type File struct {
	Rules []Rule `json:"rules"`
}

type Rule struct {
	Name       string            `json:"name"`
	Enabled    *bool             `json:"enabled,omitempty"`
	Priority   int               `json:"priority"`
	Host       string            `json:"host,omitempty"`
	HostSuffix string            `json:"hostSuffix,omitempty"`
	HostGlob   string            `json:"hostGlob,omitempty"`
	Method     string            `json:"method,omitempty"`
	Path       string            `json:"path,omitempty"`
	PathPrefix string            `json:"pathPrefix,omitempty"`
	PathRegex  string            `json:"pathRegex,omitempty"`
	Query      map[string]string `json:"query,omitempty"`
	Headers    map[string]string `json:"headers,omitempty"`
	Extract    string            `json:"extract,omitempty"`
	Action     Action            `json:"action"`

	order      int
	pathRegexp *regexp.Regexp
}

type Action struct {
	Type        string            `json:"type"`
	Status      int               `json:"status,omitempty"`
	Headers     map[string]string `json:"headers,omitempty"`
	Body        string            `json:"body,omitempty"`
	BodyFile    string            `json:"bodyFile,omitempty"`
	ContentType string            `json:"contentType,omitempty"`
	DelayMS     int               `json:"delayMs,omitempty"`
}

type Set struct {
	rules []Rule
}

func Load(path string) (*Set, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var file File
	if err := json.Unmarshal(content, &file); err != nil {
		return nil, fmt.Errorf("parse rules file: %w", err)
	}

	baseDir := filepath.Dir(path)
	return NewSet(file.Rules, baseDir)
}

func NewSet(input []Rule, baseDir string) (*Set, error) {
	compiled := make([]Rule, 0, len(input))
	for index, rule := range input {
		rule.order = index
		if err := rule.compile(baseDir); err != nil {
			return nil, err
		}
		compiled = append(compiled, rule)
	}

	sort.SliceStable(compiled, func(leftIndex, rightIndex int) bool {
		leftRule := compiled[leftIndex]
		rightRule := compiled[rightIndex]
		if leftRule.Priority == rightRule.Priority {
			return leftRule.order < rightRule.order
		}
		return leftRule.Priority > rightRule.Priority
	})

	return &Set{rules: compiled}, nil
}

func (set *Set) Match(request *http.Request) (*Rule, bool) {
	if set == nil {
		return nil, false
	}

	for index := range set.rules {
		rule := &set.rules[index]
		if rule.Matches(request) {
			return rule, true
		}
	}

	return nil, false
}

func (set *Set) HasHostMatch(host string) bool {
	if set == nil {
		return false
	}

	normalizedHost := normalizeHost(host)
	for index := range set.rules {
		rule := &set.rules[index]
		if rule.isEnabled() && rule.matchesHost(normalizedHost) {
			return true
		}
	}

	return false
}

func (set *Set) Rules() []Rule {
	if set == nil {
		return nil
	}
	copyRules := make([]Rule, len(set.rules))
	copy(copyRules, set.rules)
	return copyRules
}

func (rule *Rule) compile(baseDir string) error {
	if strings.TrimSpace(rule.Name) == "" {
		return errors.New("rule name is required")
	}
	if rule.Host == "" && rule.HostSuffix == "" && rule.HostGlob == "" {
		return fmt.Errorf("rule %q must define host, hostSuffix, or hostGlob", rule.Name)
	}
	if rule.PathRegex != "" {
		compiled, err := regexp.Compile(rule.PathRegex)
		if err != nil {
			return fmt.Errorf("rule %q has invalid pathRegex: %w", rule.Name, err)
		}
		rule.pathRegexp = compiled
	}

	rule.Method = strings.ToUpper(strings.TrimSpace(rule.Method))
	if rule.Action.Type == "" {
		rule.Action.Type = "mock"
	}
	switch rule.Action.Type {
	case "mock", "static", "modify", "passthrough":
	default:
		return fmt.Errorf("rule %q has unsupported action type %q", rule.Name, rule.Action.Type)
	}
	if rule.Action.Status == 0 {
		rule.Action.Status = http.StatusOK
	}
	if rule.Action.DelayMS < 0 {
		return fmt.Errorf("rule %q delayMs cannot be negative", rule.Name)
	}
	if rule.Extract != "" {
		switch rule.Extract {
		case "jd-cartview":
		default:
			return fmt.Errorf("rule %q has unsupported extract target %q", rule.Name, rule.Extract)
		}
	}
	if rule.Action.BodyFile != "" {
		bodyPath := rule.Action.BodyFile
		if !filepath.IsAbs(bodyPath) {
			bodyPath = filepath.Join(baseDir, bodyPath)
		}
		content, err := os.ReadFile(bodyPath)
		if err != nil {
			return fmt.Errorf("rule %q cannot read bodyFile: %w", rule.Name, err)
		}
		rule.Action.Body = string(content)
	}

	return nil
}

func (rule Rule) Matches(request *http.Request) bool {
	if !rule.isEnabled() {
		return false
	}
	if !rule.matchesHost(normalizeHost(request.Host)) {
		return false
	}
	if rule.Method != "" && strings.ToUpper(request.Method) != rule.Method {
		return false
	}
	if !rule.matchesPath(request.URL) {
		return false
	}
	if !matchesQuery(request.URL.Query(), rule.Query) {
		return false
	}
	if !matchesHeaders(request.Header, rule.Headers) {
		return false
	}

	return true
}

func (rule Rule) Delay() time.Duration {
	if rule.Action.DelayMS <= 0 {
		return 0
	}
	return time.Duration(rule.Action.DelayMS) * time.Millisecond
}

func (rule Rule) isEnabled() bool {
	return rule.Enabled == nil || *rule.Enabled
}

func (rule Rule) matchesHost(host string) bool {
	if rule.Host != "" && strings.EqualFold(host, normalizeHost(rule.Host)) {
		return true
	}
	if rule.HostSuffix != "" {
		suffix := strings.ToLower(strings.TrimPrefix(normalizeHost(rule.HostSuffix), "."))
		if host == suffix || strings.HasSuffix(host, "."+suffix) {
			return true
		}
	}
	if rule.HostGlob != "" {
		matched, _ := filepath.Match(strings.ToLower(rule.HostGlob), host)
		if matched {
			return true
		}
	}

	return false
}

func (rule Rule) matchesPath(requestURL *url.URL) bool {
	path := requestURL.Path
	if rule.Path != "" && path != rule.Path {
		return false
	}
	if rule.PathPrefix != "" && !strings.HasPrefix(path, rule.PathPrefix) {
		return false
	}
	if rule.pathRegexp != nil && !rule.pathRegexp.MatchString(path) {
		return false
	}

	return true
}

func normalizeHost(host string) string {
	host = strings.TrimSpace(strings.ToLower(host))
	if parsedHost, _, err := net.SplitHostPort(host); err == nil && parsedHost != "" {
		return strings.Trim(parsedHost, "[]")
	}
	lastColon := strings.LastIndex(host, ":")
	if lastColon > -1 && !strings.Contains(host[:lastColon], ":") {
		return strings.Trim(host[:lastColon], "[]")
	}
	return strings.Trim(host, "[]")
}

func matchesQuery(values url.Values, expected map[string]string) bool {
	for key, expectedValue := range expected {
		if values.Get(key) != expectedValue {
			return false
		}
	}
	return true
}

func matchesHeaders(headers http.Header, expected map[string]string) bool {
	for key, expectedValue := range expected {
		if headers.Get(key) != expectedValue {
			return false
		}
	}
	return true
}
