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
	BaseDir           string
	CertDir           string
	LogDir            string
	ProxyStatePath    string
	NotifyConfigPath  string
	LicenseStatePath  string
	LicenseServerPath string
	DeviceIDPath      string
}

type ServeOptions struct {
	Addr              string
	RulesPath         string
	EnableSystemProxy bool
	ProxyOverride     string
}

const (
	maxMainLogBytes   = 10 << 20
	maxMainLogBackups = 3
)

// ResolveRuntimePath resolves a relative resource first from the current
// working directory and then from the executable directory. Packaged apps can
// therefore start from shortcuts or shells whose working directory differs
// from the release directory.
func ResolveRuntimePath(path string) string {
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	cwd, _ := os.Getwd()
	executable, _ := os.Executable()
	return resolveRuntimePathFrom(path, cwd, filepath.Dir(executable))
}

func resolveRuntimePathFrom(path, cwd, executableDir string) string {
	for _, baseDir := range []string{cwd, executableDir} {
		if baseDir == "" {
			continue
		}
		candidate := filepath.Join(baseDir, path)
		if _, err := os.Stat(candidate); err == nil {
			absolute, absErr := filepath.Abs(candidate)
			if absErr == nil {
				return absolute
			}
			return filepath.Clean(candidate)
		}
	}
	return filepath.Clean(path)
}

func DefaultPaths() (Paths, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return Paths{}, err
	}
	baseDir := filepath.Join(base, "MiniProxy")
	paths := Paths{
		BaseDir:           baseDir,
		CertDir:           filepath.Join(baseDir, "certs"),
		LogDir:            filepath.Join(baseDir, "logs"),
		ProxyStatePath:    filepath.Join(baseDir, "previous-proxy.json"),
		NotifyConfigPath:  filepath.Join(baseDir, "notify.json"),
		LicenseStatePath:  filepath.Join(baseDir, "license-state.json"),
		LicenseServerPath: filepath.Join(baseDir, "license-server.txt"),
		DeviceIDPath:      filepath.Join(baseDir, "device-id"),
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
	rotateLogFile(logPath, maxMainLogBytes, maxMainLogBackups)
	file, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, nil, err
	}
	logger := log.New(file, "", log.LstdFlags|log.Lmicroseconds)
	cleanup := func() { _ = file.Close() }
	return logger, cleanup, nil
}

func rotateLogFile(path string, maxBytes int64, backups int) {
	info, err := os.Stat(path)
	if err != nil || info.Size() < maxBytes || backups < 1 {
		return
	}
	_ = os.Remove(fmt.Sprintf("%s.%d", path, backups))
	for index := backups - 1; index >= 1; index-- {
		_ = os.Rename(fmt.Sprintf("%s.%d", path, index), fmt.Sprintf("%s.%d", path, index+1))
	}
	_ = os.Rename(path, path+".1")
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
	recoverDanglingSystemProxy(paths, logger)

	options.RulesPath = ResolveRuntimePath(options.RulesPath)
	ruleSet, err := rules.Load(options.RulesPath)
	if err != nil {
		return err
	}
	certManager, err := cert.NewManager(paths.CertDir)
	if err != nil {
		return err
	}

	notifier := loadNotifier(paths.NotifyConfigPath, logger)

	server := proxy.New(proxy.Config{
		Addr:       options.Addr,
		Rules:      ruleSet,
		Certs:      certManager,
		Logger:     logger,
		Notifier:   notifier,
		CaptureDir: filepath.Join(paths.LogDir, "intercepts"),
	})

	if options.EnableSystemProxy {
		previousState, err := winproxy.Read()
		if err != nil {
			return err
		}
		if proxyPointsToLocalProxy(previousState.Server, server.Addr()) {
			previousState = winproxy.State{Override: previousState.Override}
		}
		if err := winproxy.SaveState(paths.ProxyStatePath, previousState); err != nil {
			return err
		}
		if err := winproxy.Enable(server.Addr(), options.ProxyOverride); err != nil {
			_ = winproxy.Restore(previousState)
			_ = os.Remove(paths.ProxyStatePath)
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
	if proxyPointsToLocalProxy(state.Server, addr) {
		state = winproxy.State{Override: state.Override}
	}
	if err := winproxy.SaveState(paths.ProxyStatePath, state); err != nil {
		return err
	}
	if err := winproxy.Enable(addr, override); err != nil {
		_ = winproxy.Restore(state)
		_ = os.Remove(paths.ProxyStatePath)
		return err
	}
	return nil
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
	if err := winproxy.Restore(state); err != nil {
		return err
	}
	return os.Remove(paths.ProxyStatePath)
}

func RunAutomation(path string) error {
	path = ResolveRuntimePath(path)
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
	path = ResolveRuntimePath(path)
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
		return
	}
	_ = os.Remove(paths.ProxyStatePath)
}
