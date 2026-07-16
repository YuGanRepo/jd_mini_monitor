package main

import (
	"context"
	"path/filepath"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	coreapp "mini-proxy/internal/app"
	"mini-proxy/internal/license"
	"mini-proxy/internal/notify"
	"mini-proxy/internal/proxy"
	"mini-proxy/internal/sku"
	"mini-proxy/internal/uiauto"
)

type DesktopApp struct {
	ctx     context.Context
	service *coreapp.Service
}

type DesktopStartOptions struct {
	Addr              string `json:"addr"`
	RulesPath         string `json:"rulesPath"`
	EnableSystemProxy bool   `json:"enableSystemProxy"`
	ProxyOverride     string `json:"proxyOverride"`
}

type DesktopDefaults struct {
	RulesPath      string `json:"rulesPath"`
	AutomationPath string `json:"automationPath"`
	ProxyAddr      string `json:"proxyAddr"`
	ProxyOverride  string `json:"proxyOverride"`
}

func NewDesktopApp() (*DesktopApp, error) {
	service, err := coreapp.NewService()
	if err != nil {
		return nil, err
	}
	return &DesktopApp{service: service}, nil
}

func (app *DesktopApp) Startup(ctx context.Context) {
	app.ctx = ctx
	if _, err := app.service.EnsureCert(); err != nil {
		runtime.LogError(ctx, "automatic certificate setup failed: "+err.Error())
	}
	if err := app.service.AutoStartProxy(); err != nil {
		runtime.LogError(ctx, "automatic proxy startup failed: "+err.Error())
	}
}

func (app *DesktopApp) Shutdown(ctx context.Context) {
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := app.service.Close(shutdownCtx); err != nil && app.ctx != nil {
		runtime.LogError(app.ctx, err.Error())
	}
}

func (app *DesktopApp) GetDefaults() DesktopDefaults {
	return DesktopDefaults{
		RulesPath:      coreapp.ResolveRuntimePath(filepath.Clean("configs/jd.rules.json")),
		AutomationPath: coreapp.ResolveRuntimePath(filepath.Clean("configs/example.automation.json")),
		ProxyAddr:      "127.0.0.1:8899",
		ProxyOverride:  "localhost;127.0.0.1;<local>",
	}
}

func (app *DesktopApp) GetStatus() coreapp.Status {
	return app.service.Status()
}

func (app *DesktopApp) StartProxy(options DesktopStartOptions) (coreapp.Status, error) {
	return app.service.StartProxy(coreapp.ServeOptions{
		Addr:              options.Addr,
		RulesPath:         options.RulesPath,
		EnableSystemProxy: options.EnableSystemProxy,
		ProxyOverride:     options.ProxyOverride,
	})
}

func (app *DesktopApp) StopProxy() (coreapp.Status, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return app.service.StopProxy(ctx)
}

func (app *DesktopApp) InstallCert() (coreapp.Status, error) {
	return app.service.InstallCert()
}

func (app *DesktopApp) UninstallCert() (coreapp.Status, error) {
	return app.service.UninstallCert()
}

func (app *DesktopApp) StartJDAutomation(options uiauto.CoordCycleOptions) (coreapp.JDAutomationStatus, error) {
	return app.service.StartJDAutomation(options)
}

func (app *DesktopApp) StopJDAutomation() coreapp.JDAutomationStatus {
	return app.service.StopJDAutomation()
}

func (app *DesktopApp) GetJDAutomationStatus() coreapp.JDAutomationStatus {
	return app.service.GetJDAutomationStatus()
}

func (app *DesktopApp) GetRequestLogs() []proxy.RequestLogEntry {
	return app.service.GetRequestLogs()
}

func (app *DesktopApp) GetSKUList() sku.Snapshot {
	return app.service.GetSKUList()
}

func (app *DesktopApp) ResetSKUList() sku.Snapshot {
	app.service.ResetSKUList()
	return app.service.GetSKUList()
}

func (app *DesktopApp) GetNotifyConfig() (notify.Config, error) {
	return app.service.GetNotifyConfig()
}

func (app *DesktopApp) SaveNotifyConfig(config notify.Config) error {
	return app.service.SaveNotifyConfig(config)
}

func (app *DesktopApp) TestNotify(config notify.Config) error {
	return app.service.TestNotify(config)
}

func (app *DesktopApp) ActivateLicense(licenseKey string) error {
	return app.service.ActivateLicense(licenseKey)
}

func (app *DesktopApp) VerifyLicense() (bool, error) {
	return app.service.VerifyLicense()
}

func (app *DesktopApp) DeactivateLicense() error {
	return app.service.DeactivateLicense()
}

func (app *DesktopApp) GetLicenseState() license.State {
	return app.service.GetLicenseState()
}

func (app *DesktopApp) GetDeviceID() string {
	return app.service.GetDeviceID()
}

func (app *DesktopApp) GetLicenseServerURL() string {
	return app.service.GetLicenseServerURL()
}

func (app *DesktopApp) SetLicenseServerURL(url string) error {
	return app.service.SetLicenseServerURL(url)
}
