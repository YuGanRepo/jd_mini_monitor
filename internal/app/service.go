package app

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"

	"mini-proxy/internal/cert"
	"mini-proxy/internal/proxy"
	"mini-proxy/internal/rules"
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

	return &Service{
		paths:         paths,
		logger:        logger,
		cleanupLogger: cleanup,
		certManager:   certManager,
		addr:          "127.0.0.1:8899",
		rulesPath:     "configs/example.rules.json",
	}, nil
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
		options.RulesPath = "configs/example.rules.json"
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

	proxyServer := proxy.New(proxy.Config{
		Addr:   options.Addr,
		Rules:  ruleSet,
		Certs:  service.certManager,
		Logger: service.logger,
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
	ctx, cancel := context.WithCancel(context.Background())
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
