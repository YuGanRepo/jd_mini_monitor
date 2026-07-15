package app

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"mini-proxy/internal/cert"
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
	skuStore          *sku.Store
	rulesPath         string
	addr              string
	systemProxyActive bool
	lastError         string

	jdAutomationCancel context.CancelFunc
	jdAutomationStatus JDAutomationStatus
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

	// If a previous run enabled the Windows system proxy and then exited
	// uncleanly (crash / force-kill), the system proxy is left pointing at our
	// now-dead listener, which breaks all internet access. Recover it here.
	recoverDanglingSystemProxy(paths, logger)

	return &Service{
		paths:         paths,
		logger:        logger,
		cleanupLogger: cleanup,
		certManager:   certManager,
		skuStore:      skuStore,
		addr:          "127.0.0.1:8899",
		rulesPath:     "configs/jd.rules.json",
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
	service.mu.Unlock()
	if store != nil {
		store.Reset()
	}
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

func (service *Service) StartProxy(options ServeOptions) (Status, error) {
	service.mu.Lock()
	if service.proxyServer != nil {
		service.mu.Unlock()
		return service.Status(), fmt.Errorf("proxy is already running")
	}
	if options.Addr == "" {
		options.Addr = "127.0.0.1:8899"
	}
	if options.RulesPath == "" {
		options.RulesPath = "configs/jd.rules.json"
	}
	if options.ProxyOverride == "" {
		options.ProxyOverride = "localhost;127.0.0.1;<local>"
	}
	service.mu.Unlock()

	ruleSet, err := rules.Load(options.RulesPath)
	if err != nil {
		service.setLastError(err)
		return service.Status(), err
	}

	if !service.certManager.IsTrustedRootInstalled() {
		if err := service.certManager.InstallTrustedRoot(); err != nil {
			service.setLastError(err)
			return service.Status(), err
		}
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
			service.setLastError(err)
		}
	}()

	return service.Status(), nil
}

func (service *Service) StopProxy(ctx context.Context) (Status, error) {
	service.mu.Lock()
	proxyServer := service.proxyServer
	systemProxyActive := service.systemProxyActive
	service.proxyServer = nil
	service.systemProxyActive = false
	service.mu.Unlock()

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

func (service *Service) InstallCert() (Status, error) {
	err := service.certManager.InstallTrustedRoot()
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
	service.mu.Lock()
	if service.jdAutomationCancel != nil {
		service.mu.Unlock()
		return service.GetJDAutomationStatus(), fmt.Errorf("JD automation is already running")
	}
	service.mu.Unlock()

	// Pre-check that the target mini-program window is already open, so the user
	// gets an immediate, clear prompt instead of the run failing partway through.
	if err := uiauto.CheckWindowAvailable(options); err != nil {
		service.mu.Lock()
		service.jdAutomationStatus = JDAutomationStatus{Running: false, LastError: err.Error()}
		service.mu.Unlock()
		return service.GetJDAutomationStatus(), err
	}

	ctx, cancel := context.WithCancel(context.Background())
	service.mu.Lock()
	service.jdAutomationCancel = cancel
	service.jdAutomationStatus = JDAutomationStatus{Running: true, TotalCycles: options.RepeatCount}
	service.mu.Unlock()

	go func() {
		err := uiauto.RunCoordCycle(ctx, options, service.logger, func(cycle int) {
			service.mu.Lock()
			service.jdAutomationStatus.CurrentCycle = cycle
			service.mu.Unlock()
		})

		service.mu.Lock()
		service.jdAutomationCancel = nil
		service.jdAutomationStatus.Running = false
		if err != nil && err != context.Canceled {
			service.jdAutomationStatus.LastError = err.Error()
		} else {
			service.jdAutomationStatus.LastError = ""
		}
		service.mu.Unlock()
	}()

	return service.GetJDAutomationStatus(), nil
}

// StopJDAutomation cancels a running JD automation cycle, if any. It is safe to call
// even when nothing is running.
func (service *Service) StopJDAutomation() JDAutomationStatus {
	service.mu.Lock()
	cancel := service.jdAutomationCancel
	service.mu.Unlock()
	if cancel != nil {
		cancel()
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
