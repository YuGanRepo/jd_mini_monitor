<#
.SYNOPSIS
  Opens the "购物车" (shopping cart) tab inside an already-open WeChat mini program window
  (e.g. the JD/京东购物 mini program) by simulating a mouse click on its bottom nav tab.

.DESCRIPTION
  WeChat mini programs run as custom-drawn Chromium content inside a `WeChatAppEx.exe`
  window and do not expose a usable UI Automation control tree (FindAll Descendants
  returns effectively nothing), so the existing internal/uiauto button-name/automationId
  automation cannot target them. This script instead locates the mini program window by
  title, brings it to the foreground, and clicks a fixed relative position on its bottom
  navigation bar where the cart tab sits (5-tab layout: 首页 / 逛 / 超级会场 / 购物车 / 我的).

  Coordinates are computed as a ratio of the window's client rect so the click still lands
  correctly if the window is moved or resized, as long as the bottom-nav layout is unchanged.

.PARAMETER ProcessName
  Process that hosts the mini program window. WeChat mini programs run under WeChatAppEx.

.PARAMETER WindowTitleContains
  Substring to match against the window title, e.g. "京东" for the JD mini program.

.PARAMETER CartXRatio
  Horizontal position of the cart tab as a fraction of window width (0-1).
  Verified value for the 5-tab JD mini program bottom nav (4th tab of 5): 0.70

.PARAMETER CartYRatio
  Vertical position of the cart tab as a fraction of window height (0-1).
  Verified value for the JD mini program bottom nav: 0.95

.EXAMPLE
  .\scripts\wechat-open-miniprogram-cart.ps1
  .\scripts\wechat-open-miniprogram-cart.ps1 -WindowTitleContains "京东"

.NOTES
  - Requires the mini program window to already be open (this script does not open it).
  - Desktop windows can shift z-order/focus outside of this script's control (e.g. the
    user actively using the machine). Re-run if the click misses, and confirm the target
    window is visible/foreground immediately before running.
  - This only navigates to the cart tab; it never confirms orders or submits payments.
#>
param(
  [string]$ProcessName = "WeChatAppEx",
  [string]$WindowTitleContains = "京东",
  [double]$CartXRatio = 0.70,
  [double]$CartYRatio = 0.95
)

$ErrorActionPreference = "Stop"

Add-Type -AssemblyName UIAutomationClient
Add-Type -AssemblyName UIAutomationTypes
Add-Type -Namespace MiniProxy -Name Win32 -MemberDefinition @'
[System.Runtime.InteropServices.DllImport("user32.dll")]
public static extern bool SetForegroundWindow(System.IntPtr hWnd);
[System.Runtime.InteropServices.DllImport("user32.dll")]
public static extern bool ShowWindow(System.IntPtr hWnd, int nCmdShow);
[System.Runtime.InteropServices.DllImport("user32.dll")]
public static extern bool GetWindowRect(System.IntPtr hWnd, out RECT rect);
[System.Runtime.InteropServices.DllImport("user32.dll")]
public static extern bool SetCursorPos(int X, int Y);
[System.Runtime.InteropServices.DllImport("user32.dll")]
public static extern void mouse_event(int dwFlags, int dx, int dy, int dwData, int dwExtraInfo);
public struct RECT { public int Left; public int Top; public int Right; public int Bottom; }
'@

$root = [System.Windows.Automation.AutomationElement]::RootElement
$windows = $root.FindAll([System.Windows.Automation.TreeScope]::Children, [System.Windows.Automation.Condition]::TrueCondition)

$target = $null
foreach ($window in $windows) {
  $name = $window.Current.Name
  if (-not $name -or $name -notlike "*$WindowTitleContains*") { continue }
  try {
    $process = [System.Diagnostics.Process]::GetProcessById($window.Current.ProcessId).ProcessName
  } catch {
    continue
  }
  if ($process -ne $ProcessName) { continue }
  $target = $window
  break
}

if (-not $target) {
  throw "No window found with title containing '$WindowTitleContains' hosted by process '$ProcessName'. Make sure the mini program is already open."
}

$handle = [IntPtr]$target.Current.NativeWindowHandle
[MiniProxy.Win32]::ShowWindow($handle, 9) | Out-Null   # SW_RESTORE
[MiniProxy.Win32]::SetForegroundWindow($handle) | Out-Null
Start-Sleep -Milliseconds 500

$rect = New-Object MiniProxy.Win32+RECT
[MiniProxy.Win32]::GetWindowRect($handle, [ref]$rect) | Out-Null
$width = $rect.Right - $rect.Left
$height = $rect.Bottom - $rect.Top
if ($width -le 0 -or $height -le 0) {
  throw "Window '$($target.Current.Name)' has no usable size (is it minimized or offscreen?)."
}

$x = [int]($rect.Left + ($width * $CartXRatio))
$y = [int]($rect.Top + ($height * $CartYRatio))

[MiniProxy.Win32]::SetCursorPos($x, $y) | Out-Null
Start-Sleep -Milliseconds 200
[MiniProxy.Win32]::mouse_event(0x0002, 0, 0, 0, 0)  # left button down
[MiniProxy.Win32]::mouse_event(0x0004, 0, 0, 0, 0)  # left button up

Write-Output "Clicked cart tab at ($x, $y) in window '$($target.Current.Name)' [$width x $height]"
