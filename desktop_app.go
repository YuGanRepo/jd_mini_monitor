package main

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	coreapp "mini-proxy/internal/app"
	"mini-proxy/internal/proxy"
	"mini-proxy/internal/rules"
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

type RulesValidationResult struct {
	Valid bool   `json:"valid"`
	Count int    `json:"count"`
	Error string `json:"error"`
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
		RulesPath:      filepath.Clean("configs/jd.rules.json"),
		AutomationPath: filepath.Clean("configs/example.automation.json"),
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

func (app *DesktopApp) ReadTextFile(path string) (string, error) {
	return app.service.ReadTextFile(path)
}

func (app *DesktopApp) WriteTextFile(path string, content string) error {
	return app.service.WriteTextFile(path, content)
}

func (app *DesktopApp) ValidateRulesText(content string) RulesValidationResult {
	var file rules.File
	if err := json.Unmarshal([]byte(content), &file); err != nil {
		return RulesValidationResult{Valid: false, Error: err.Error()}
	}
	set, err := rules.NewSet(file.Rules, ".")
	if err != nil {
		return RulesValidationResult{Valid: false, Error: err.Error()}
	}
	return RulesValidationResult{Valid: true, Count: len(set.Rules())}
}

func (app *DesktopApp) FormatJSON(content string) (string, error) {
	var value any
	if err := json.Unmarshal([]byte(content), &value); err != nil {
		return "", err
	}
	formatted, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return "", err
	}
	return string(formatted), nil
}

func (app *DesktopApp) SelectJSONFile(title string) (string, error) {
	if app.ctx == nil {
		return "", fmt.Errorf("desktop context is not ready")
	}
	return runtime.OpenFileDialog(app.ctx, runtime.OpenDialogOptions{
		Title: title,
		Filters: []runtime.FileFilter{
			{DisplayName: "JSON files", Pattern: "*.json"},
			{DisplayName: "All files", Pattern: "*.*"},
		},
	})
}

func (app *DesktopApp) InspectAutomation(path string) (string, error) {
	return app.service.InspectAutomation(path)
}

func (app *DesktopApp) RunAutomation(path string) (coreapp.Status, error) {
	err := app.service.RunAutomation(path)
	return app.service.Status(), err
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
