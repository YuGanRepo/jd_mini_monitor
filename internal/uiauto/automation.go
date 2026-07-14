package uiauto

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

type Config struct {
	Sequences []Sequence `json:"sequences"`
}

type Sequence struct {
	Name       string         `json:"name"`
	Window     WindowSelector `json:"window"`
	Steps      []Step         `json:"steps"`
	Repeat     int            `json:"repeat,omitempty"`
	IntervalMS int            `json:"intervalMs,omitempty"`
}

type WindowSelector struct {
	Title         string `json:"title,omitempty"`
	TitleContains string `json:"titleContains,omitempty"`
	Process       string `json:"process,omitempty"`
	TimeoutMS     int    `json:"timeoutMs,omitempty"`
}

type Step struct {
	Name             string `json:"name,omitempty"`
	ButtonName       string `json:"buttonName,omitempty"`
	ButtonContains   string `json:"buttonContains,omitempty"`
	AutomationID     string `json:"automationId,omitempty"`
	DelayMS          int    `json:"delayMs,omitempty"`
	TimeoutMS        int    `json:"timeoutMs,omitempty"`
	Retry            int    `json:"retry,omitempty"`
	FallbackToCursor bool   `json:"fallbackToCursor,omitempty"`
}

func Load(path string) (Config, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	var config Config
	if err := json.Unmarshal(content, &config); err != nil {
		return Config{}, err
	}
	return config, config.Validate()
}

func (config Config) Validate() error {
	if len(config.Sequences) == 0 {
		return fmt.Errorf("automation config must include at least one sequence")
	}
	for sequenceIndex, sequence := range config.Sequences {
		if sequence.Name == "" {
			return fmt.Errorf("sequence %d name is required", sequenceIndex)
		}
		if sequence.Window.Title == "" && sequence.Window.TitleContains == "" && sequence.Window.Process == "" {
			return fmt.Errorf("sequence %q must define a window selector", sequence.Name)
		}
		if len(sequence.Steps) == 0 {
			return fmt.Errorf("sequence %q must define at least one step", sequence.Name)
		}
		for stepIndex, step := range sequence.Steps {
			if step.ButtonName == "" && step.ButtonContains == "" && step.AutomationID == "" {
				return fmt.Errorf("sequence %q step %d must define buttonName, buttonContains, or automationId", sequence.Name, stepIndex)
			}
		}
	}
	return nil
}

func Inspect(ctx context.Context, selector WindowSelector) (string, error) {
	if runtime.GOOS != "windows" {
		return "", fmt.Errorf("UI automation is only supported on Windows")
	}
	config := Config{Sequences: []Sequence{{Name: "inspect", Window: selector, Steps: []Step{{ButtonContains: "*"}}}}}
	path, cleanup, err := writeTempJSON(config)
	if err != nil {
		return "", err
	}
	defer cleanup()
	return runPowerShell(ctx, inspectScript, path)
}

func Run(ctx context.Context, config Config, logger *log.Logger) error {
	if runtime.GOOS != "windows" {
		return fmt.Errorf("UI automation is only supported on Windows")
	}
	if err := config.Validate(); err != nil {
		return err
	}
	path, cleanup, err := writeTempJSON(config)
	if err != nil {
		return err
	}
	defer cleanup()

	output, err := runPowerShell(ctx, runScript, path)
	if logger != nil && strings.TrimSpace(output) != "" {
		logger.Print(strings.TrimSpace(output))
	}
	return err
}

func writeTempJSON(value any) (string, func(), error) {
	content, err := json.Marshal(value)
	if err != nil {
		return "", nil, err
	}
	path := filepath.Join(os.TempDir(), fmt.Sprintf("mini-proxy-uiauto-%d.json", time.Now().UnixNano()))
	if err := os.WriteFile(path, content, 0o600); err != nil {
		return "", nil, err
	}
	cleanup := func() { _ = os.Remove(path) }
	return path, cleanup, nil
}

func runPowerShell(ctx context.Context, script string, configPath string) (string, error) {
	scriptPath := filepath.Join(os.TempDir(), fmt.Sprintf("mini-proxy-uiauto-%d.ps1", time.Now().UnixNano()))
	if err := os.WriteFile(scriptPath, []byte(script), 0o600); err != nil {
		return "", err
	}
	defer os.Remove(scriptPath)

	command := exec.CommandContext(ctx, "powershell", "-NoProfile", "-ExecutionPolicy", "Bypass", "-File", scriptPath, configPath)
	output, err := command.CombinedOutput()
	if err != nil {
		return string(output), fmt.Errorf("PowerShell UI automation failed: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return string(output), nil
}

const inspectScript = `
param([string]$ConfigPath)
Add-Type -AssemblyName UIAutomationClient
Add-Type -AssemblyName UIAutomationTypes
$config = Get-Content -Raw $ConfigPath | ConvertFrom-Json
$selector = $config.sequences[0].window

function Test-WindowMatch($window, $selector) {
  $name = $window.Current.Name
  if ($selector.title -and $name -ne $selector.title) { return $false }
  if ($selector.titleContains -and $name -notlike "*$($selector.titleContains)*") { return $false }
  if ($selector.process) {
    try { $process = [System.Diagnostics.Process]::GetProcessById($window.Current.ProcessId).ProcessName } catch { return $false }
    if ($process -ne $selector.process) { return $false }
  }
  return $true
}

$root = [System.Windows.Automation.AutomationElement]::RootElement
$windows = $root.FindAll([System.Windows.Automation.TreeScope]::Children, [System.Windows.Automation.Condition]::TrueCondition)
$result = @()
foreach ($window in $windows) {
  if (-not (Test-WindowMatch $window $selector)) { continue }
  $buttons = $window.FindAll([System.Windows.Automation.TreeScope]::Descendants, (New-Object System.Windows.Automation.PropertyCondition([System.Windows.Automation.AutomationElement]::ControlTypeProperty, [System.Windows.Automation.ControlType]::Button)))
  foreach ($button in $buttons) {
    $result += [pscustomobject]@{
      window = $window.Current.Name
      processId = $window.Current.ProcessId
      name = $button.Current.Name
      automationId = $button.Current.AutomationId
      className = $button.Current.ClassName
      enabled = $button.Current.IsEnabled
    }
  }
}
$result | ConvertTo-Json -Depth 4
`

const runScript = `
param([string]$ConfigPath)
Add-Type -AssemblyName UIAutomationClient
Add-Type -AssemblyName UIAutomationTypes
Add-Type -AssemblyName System.Windows.Forms
Add-Type -AssemblyName System.Drawing
Add-Type -Namespace MiniProxy -Name NativeMouse -MemberDefinition @'
[System.Runtime.InteropServices.DllImport("user32.dll")]
public static extern bool SetCursorPos(int X, int Y);
[System.Runtime.InteropServices.DllImport("user32.dll")]
public static extern void mouse_event(int dwFlags, int dx, int dy, int dwData, int dwExtraInfo);
'@
$config = Get-Content -Raw $ConfigPath | ConvertFrom-Json

function Test-WindowMatch($window, $selector) {
  $name = $window.Current.Name
  if ($selector.title -and $name -ne $selector.title) { return $false }
  if ($selector.titleContains -and $name -notlike "*$($selector.titleContains)*") { return $false }
  if ($selector.process) {
    try { $process = [System.Diagnostics.Process]::GetProcessById($window.Current.ProcessId).ProcessName } catch { return $false }
    if ($process -ne $selector.process) { return $false }
  }
  return $true
}

function Find-Window($selector) {
  $timeout = 10000
  if ($selector.timeoutMs) { $timeout = [int]$selector.timeoutMs }
  $deadline = [DateTime]::UtcNow.AddMilliseconds($timeout)
  $root = [System.Windows.Automation.AutomationElement]::RootElement
  do {
    $windows = $root.FindAll([System.Windows.Automation.TreeScope]::Children, [System.Windows.Automation.Condition]::TrueCondition)
    foreach ($window in $windows) {
      if (Test-WindowMatch $window $selector) { return $window }
    }
    Start-Sleep -Milliseconds 250
  } while ([DateTime]::UtcNow -lt $deadline)
  throw "Window not found"
}

function Test-ButtonMatch($button, $step) {
  if ($step.automationId -and $button.Current.AutomationId -ne $step.automationId) { return $false }
  if ($step.buttonName -and $button.Current.Name -ne $step.buttonName) { return $false }
  if ($step.buttonContains -and $step.buttonContains -ne "*" -and $button.Current.Name -notlike "*$($step.buttonContains)*") { return $false }
  return $true
}

function Find-Button($window, $step) {
  $timeout = 5000
  if ($step.timeoutMs) { $timeout = [int]$step.timeoutMs }
  $deadline = [DateTime]::UtcNow.AddMilliseconds($timeout)
  $condition = New-Object System.Windows.Automation.PropertyCondition([System.Windows.Automation.AutomationElement]::ControlTypeProperty, [System.Windows.Automation.ControlType]::Button)
  do {
    $buttons = $window.FindAll([System.Windows.Automation.TreeScope]::Descendants, $condition)
    foreach ($button in $buttons) {
      if (Test-ButtonMatch $button $step) { return $button }
    }
    Start-Sleep -Milliseconds 200
  } while ([DateTime]::UtcNow -lt $deadline)
  throw "Button not found for step $($step.name)"
}

function Invoke-Button($button, $fallbackToCursor) {
  $patternObject = $null
  if ($button.TryGetCurrentPattern([System.Windows.Automation.InvokePattern]::Pattern, [ref]$patternObject)) {
    $patternObject.Invoke()
    return
  }
  if (-not $fallbackToCursor) { throw "Button does not support InvokePattern and fallbackToCursor is false" }
  $rect = $button.Current.BoundingRectangle
  $x = [int]($rect.Left + ($rect.Width / 2))
  $y = [int]($rect.Top + ($rect.Height / 2))
  [MiniProxy.NativeMouse]::SetCursorPos($x, $y) | Out-Null
  [MiniProxy.NativeMouse]::mouse_event(0x0002, 0, 0, 0, 0)
  [MiniProxy.NativeMouse]::mouse_event(0x0004, 0, 0, 0, 0)
}

foreach ($sequence in $config.sequences) {
  $repeat = 1
  if ($sequence.repeat -and [int]$sequence.repeat -gt 0) { $repeat = [int]$sequence.repeat }
  for ($iteration = 0; $iteration -lt $repeat; $iteration++) {
    $window = Find-Window $sequence.window
    foreach ($step in $sequence.steps) {
      if ($step.delayMs) { Start-Sleep -Milliseconds ([int]$step.delayMs) }
      $retry = 0
      if ($step.retry -and [int]$step.retry -gt 0) { $retry = [int]$step.retry }
      for ($attempt = 0; $attempt -le $retry; $attempt++) {
        try {
          $button = Find-Button $window $step
          Invoke-Button $button $step.fallbackToCursor
          Write-Output "clicked sequence=$($sequence.name) step=$($step.name) button=$($button.Current.Name) automationId=$($button.Current.AutomationId)"
          break
        } catch {
          if ($attempt -eq $retry) { throw }
          Start-Sleep -Milliseconds 250
        }
      }
    }
    if ($sequence.intervalMs -and $iteration -lt ($repeat - 1)) { Start-Sleep -Milliseconds ([int]$sequence.intervalMs) }
  }
}
`
