package app

import (
	"context"
	"io"
	"log"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"mini-proxy/internal/cert"
	"mini-proxy/internal/license"
	"mini-proxy/internal/sku"
)

func TestStartProxyReturnsRuleLoadErrorWithoutPanic(t *testing.T) {
	certManager, err := cert.NewManager(t.TempDir())
	if err != nil {
		t.Fatalf("cert.NewManager() error = %v", err)
	}

	service := &Service{
		logger:       log.New(io.Discard, "", 0),
		certManager:  certManager,
		skuStore:     sku.NewStore(),
		licenseStore: license.NewStore(filepath.Join(t.TempDir(), "license-state.json")),
	}

	_, err = service.StartProxy(ServeOptions{RulesPath: filepath.Join(t.TempDir(), "missing-rules.json")})
	if err == nil {
		t.Fatal("StartProxy() error = nil, want missing rules error")
	}
}

func TestProxyPointsToLocalProxy(t *testing.T) {
	tests := []struct {
		name   string
		server string
		addr   string
		want   bool
	}{
		{name: "exact address", server: "127.0.0.1:9000", addr: "127.0.0.1:9000", want: true},
		{name: "protocol mapping", server: "http=127.0.0.1:8899;https=127.0.0.1:8899", want: true},
		{name: "other proxy", server: "proxy.example:8080", addr: "127.0.0.1:8899", want: false},
		{name: "empty", server: "", addr: "127.0.0.1:8899", want: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := proxyPointsToLocalProxy(test.server, test.addr); got != test.want {
				t.Fatalf("proxyPointsToLocalProxy(%q, %q) = %t, want %t", test.server, test.addr, got, test.want)
			}
		})
	}
}

func TestResetSKUListRemovesPersistedSnapshot(t *testing.T) {
	logDir := t.TempDir()
	snapshotPath := filepath.Join(logDir, "intercepts", "sku-latest.json")
	if err := os.MkdirAll(filepath.Dir(snapshotPath), 0o700); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(snapshotPath, []byte(`{"entries":[]}`), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	store := sku.NewStore()
	store.Update([]sku.SKU{{ItemID: "1", Name: "test", FinalPriceCents: 100}})
	service := &Service{
		paths:    Paths{LogDir: logDir},
		logger:   log.New(io.Discard, "", 0),
		skuStore: store,
	}

	service.ResetSKUList()
	if got := service.GetSKUList().TotalSKU; got != 0 {
		t.Fatalf("TotalSKU = %d, want 0", got)
	}
	if _, err := os.Stat(snapshotPath); !os.IsNotExist(err) {
		t.Fatalf("snapshot still exists after reset, stat error = %v", err)
	}
}

func TestResolveRuntimePathFromExecutableDirectory(t *testing.T) {
	cwd := t.TempDir()
	executableDir := t.TempDir()
	configPath := filepath.Join(executableDir, "configs", "jd.rules.json")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o700); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(configPath, []byte(`{"rules":[]}`), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	got := resolveRuntimePathFrom(filepath.Join("configs", "jd.rules.json"), cwd, executableDir)
	want, err := filepath.Abs(configPath)
	if err != nil {
		t.Fatalf("Abs() error = %v", err)
	}
	if got != want {
		t.Fatalf("resolveRuntimePathFrom() = %q, want %q", got, want)
	}
}

func TestReserveJDAutomationStartAllowsOnlyOneConcurrentCaller(t *testing.T) {
	service := &Service{}
	const callers = 32
	start := make(chan struct{})
	results := make(chan bool, callers)
	var waitGroup sync.WaitGroup

	for index := 0; index < callers; index++ {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			<-start
			results <- service.reserveJDAutomationStart()
		}()
	}
	close(start)
	waitGroup.Wait()
	close(results)

	reserved := 0
	for result := range results {
		if result {
			reserved++
		}
	}
	if reserved != 1 {
		t.Fatalf("successful reservations = %d, want 1", reserved)
	}
	service.releaseJDAutomationStart()
	if !service.reserveJDAutomationStart() {
		t.Fatal("reservation should be available after release")
	}
}

func TestStartProxyRejectsWhileStopping(t *testing.T) {
	certManager, err := cert.NewManager(t.TempDir())
	if err != nil {
		t.Fatalf("cert.NewManager() error = %v", err)
	}
	service := &Service{
		proxyStopping: true,
		certManager:   certManager,
		licenseStore:  license.NewStore(filepath.Join(t.TempDir(), "license-state.json")),
	}
	_, err = service.StartProxy(ServeOptions{RulesPath: filepath.Join(t.TempDir(), "missing-rules.json")})
	if err == nil || err.Error() != "proxy is still stopping" {
		t.Fatalf("StartProxy() error = %v, want proxy is still stopping", err)
	}
}

func TestAutoStartProxySkipsUnlicensedDevice(t *testing.T) {
	service := &Service{
		licenseStore: license.NewStore(filepath.Join(t.TempDir(), "license-state.json")),
		deviceID:     "unlicensed-device",
	}
	if err := service.AutoStartProxy(); err != nil {
		t.Fatalf("AutoStartProxy() error = %v", err)
	}
	if service.proxyServer != nil || service.proxyStarting || service.systemProxyActive {
		t.Fatal("unlicensed auto-start changed proxy state")
	}
}

func TestStopJDAutomationCancelsPendingStart(t *testing.T) {
	service := &Service{}
	if !service.reserveJDAutomationStart() {
		t.Fatal("initial reservation failed")
	}
	status := service.StopJDAutomation()
	if status.Running {
		t.Fatal("pending automation should not report running")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if service.completeJDAutomationStart(cancel, make(chan struct{}), 1) {
		t.Fatal("pending start completed after a stop request")
	}
	if ctx.Err() != nil {
		t.Fatalf("test context was canceled unexpectedly: %v", ctx.Err())
	}
	service.releaseJDAutomationStart()
}
