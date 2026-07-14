package app

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"mini-proxy/internal/cert"
	"mini-proxy/internal/proxy"
	"mini-proxy/internal/rules"
	"mini-proxy/internal/uiauto"
	"mini-proxy/internal/winproxy"
)

type Paths struct {
	BaseDir        string
	CertDir        string
	LogDir         string
	ProxyStatePath string
}

type ServeOptions struct {
	Addr              string
	RulesPath         string
	EnableSystemProxy bool
	ProxyOverride     string
}

func DefaultPaths() (Paths, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return Paths{}, err
	}
	baseDir := filepath.Join(base, "MiniProxy")
	paths := Paths{
		BaseDir:        baseDir,
		CertDir:        filepath.Join(baseDir, "certs"),
		LogDir:         filepath.Join(baseDir, "logs"),
		ProxyStatePath: filepath.Join(baseDir, "previous-proxy.json"),
	}
	for _, dir := range []string{paths.BaseDir, paths.CertDir, paths.LogDir} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return Paths{}, err
		}
	}
	return paths, nil
}

func NewLogger(paths Paths) (*log.Logger, func(), error) {
	logPath := filepath.Join(paths.LogDir, "mini-proxy.log")
	file, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, nil, err
	}
	logger := log.New(file, "", log.LstdFlags|log.Lmicroseconds)
	cleanup := func() { _ = file.Close() }
	return logger, cleanup, nil
}

func Serve(options ServeOptions) error {
	paths, err := DefaultPaths()
	if err != nil {
		return err
	}
	logger, cleanup, err := NewLogger(paths)
	if err != nil {
		return err
	}
	defer cleanup()

	ruleSet, err := rules.Load(options.RulesPath)
	if err != nil {
		return err
	}
	certManager, err := cert.NewManager(paths.CertDir)
	if err != nil {
		return err
	}

	server := proxy.New(proxy.Config{
		Addr:       options.Addr,
		Rules:      ruleSet,
		Certs:      certManager,
		Logger:     logger,
		CaptureDir: filepath.Join(paths.LogDir, "intercepts"),
	})

	if options.EnableSystemProxy {
		previousState, err := winproxy.Read()
		if err != nil {
			return err
		}
		if err := winproxy.SaveState(paths.ProxyStatePath, previousState); err != nil {
			return err
		}
		if err := winproxy.Enable(server.Addr(), options.ProxyOverride); err != nil {
			return err
		}
		defer restoreProxy(paths, logger)
	}

	errCh := make(chan error, 1)
	go func() { errCh <- server.ListenAndServe() }()

	stopCh := make(chan os.Signal, 1)
	signal.Notify(stopCh, os.Interrupt, syscall.SIGTERM)
	fmt.Printf("mini-proxy listening on %s\n", server.Addr())
	fmt.Printf("root certificate: %s\n", certManager.RootCertPath())

	select {
	case err := <-errCh:
		return err
	case <-stopCh:
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return server.Shutdown(ctx)
	}
}

func InstallCert() error {
	paths, err := DefaultPaths()
	if err != nil {
		return err
	}
	manager, err := cert.NewManager(paths.CertDir)
	if err != nil {
		return err
	}
	return manager.InstallTrustedRoot()
}

func UninstallCert() error {
	paths, err := DefaultPaths()
	if err != nil {
		return err
	}
	manager, err := cert.NewManager(paths.CertDir)
	if err != nil {
		return err
	}
	return manager.UninstallTrustedRoot()
}

func CertStatus() error {
	paths, err := DefaultPaths()
	if err != nil {
		return err
	}
	manager, err := cert.NewManager(paths.CertDir)
	if err != nil {
		return err
	}
	fmt.Printf("root certificate: %s\n", manager.RootCertPath())
	fmt.Printf("thumbprint: %s\n", manager.Thumbprint())
	fmt.Printf("trusted: %t\n", manager.IsTrustedRootInstalled())
	return nil
}

func EnableSystemProxy(addr string, override string) error {
	paths, err := DefaultPaths()
	if err != nil {
		return err
	}
	state, err := winproxy.Read()
	if err != nil {
		return err
	}
	if err := winproxy.SaveState(paths.ProxyStatePath, state); err != nil {
		return err
	}
	return winproxy.Enable(addr, override)
}

func RestoreSystemProxy() error {
	paths, err := DefaultPaths()
	if err != nil {
		return err
	}
	state, err := winproxy.LoadState(paths.ProxyStatePath)
	if err != nil {
		return err
	}
	return winproxy.Restore(state)
}

func RunAutomation(path string) error {
	config, err := uiauto.Load(path)
	if err != nil {
		return err
	}
	paths, err := DefaultPaths()
	if err != nil {
		return err
	}
	logger, cleanup, err := NewLogger(paths)
	if err != nil {
		return err
	}
	defer cleanup()
	return uiauto.Run(context.Background(), config, logger)
}

func InspectAutomation(path string) error {
	config, err := uiauto.Load(path)
	if err != nil {
		return err
	}
	if len(config.Sequences) == 0 {
		return fmt.Errorf("automation config must include at least one sequence")
	}
	output, err := uiauto.Inspect(context.Background(), config.Sequences[0].Window)
	if output != "" {
		fmt.Print(output)
	}
	return err
}

func restoreProxy(paths Paths, logger *log.Logger) {
	state, err := winproxy.LoadState(paths.ProxyStatePath)
	if err != nil {
		logger.Printf("load previous proxy state failed: %v", err)
		return
	}
	if err := winproxy.Restore(state); err != nil {
		logger.Printf("restore previous proxy state failed: %v", err)
	}
}
