<#
.SYNOPSIS
  Automates the WeChat JD (京东) mini program: opens the cart tab, then cycles between the
  "全部" (All) and "服务" (Service) sub-tabs on the cart page.

.DESCRIPTION
  WeChat mini programs (hosted by WeChatAppEx.exe) are custom-drawn Chromium content and do
  not expose a usable UI Automation control tree, so clicks are simulated at fixed positions
  computed as a ratio of the mini program window's client rect. This keeps clicks correct if
  the window is moved/resized, as long as the page layout itself doesn't change.

  Sequence performed once per cycle (steps 2-5 from the spec), after bringing the mini
  program window to the foreground once (step 1):
    2. Click the bottom-nav "购物车" (cart) tab
    3. Click the "全部" (All) sub-tab
    4. Wait 30s, click the "服务" (Service) sub-tab
    5. Wait 5s, click the "全部" (All) sub-tab again

  All coordinates were measured against a 430x788 mini program window using a zoomed
  screenshot; re-measure and adjust the ratio parameters below if JD changes its layout.

.PARAMETER ProcessName
  Process that hosts the mini program window. WeChat mini programs run under WeChatAppEx.

.PARAMETER WindowTitleContains
  Substring to match against the window title, e.g. "京东" for the JD mini program.

.PARAMETER RepeatCount
  Number of times to repeat steps 2-5 (cart tab -> 全部 -> wait 30s -> 服务 -> wait 5s -> 全部).

.PARAMETER FirstDelaySeconds
  Delay (seconds) between clicking 全部 and clicking 服务. Verified value: 30.

.PARAMETER SecondDelaySeconds
  Delay (seconds) between clicking 服务 and clicking 全部 again. Verified value: 5.

.EXAMPLE
  .\scripts\wechat-jd-cart-cycle.ps1
  .\scripts\wechat-jd-cart-cycle.ps1 -RepeatCount 3

.NOTES
  - Requires the JD mini program window to already be open.
  - Desktop window z-order/focus can shift outside of this script's control; re-run if a
    click misses and re-verify the ratio parameters with a fresh zoomed screenshot.
  - This only navigates between cart sub-tabs; it never confirms orders or submits payments.
#>
param(
  [string]$ProcessName = "WeChatAppEx",
  [string]$WindowTitleContains = "京东",
  [int]$RepeatCount = 1,
  [double]$CartTabXRatio = 0.70,
  [double]$CartTabYRatio = 0.95,
  [double]$AllTabXRatio = 0.10,
  [double]$AllTabYRatio = 0.108,
  [double]$ServiceTabXRatio = 0.62,
  [double]$ServiceTabYRatio = 0.108,
  [int]$FirstDelaySeconds = 30,
  [int]$SecondDelaySeconds = 5
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

function Find-MiniProgramWindow {
  $root = [System.Windows.Automation.AutomationElement]::RootElement
  $windows = $root.FindAll([System.Windows.Automation.TreeScope]::Children, [System.Windows.Automation.Condition]::TrueCondition)
  foreach ($window in $windows) {
    $name = $window.Current.Name
    if (-not $name -or $name -notlike "*$WindowTitleContains*") { continue }
    try {
      $process = [System.Diagnostics.Process]::GetProcessById($window.Current.ProcessId).ProcessName
    } catch {
      continue
    }
    if ($process -ne $ProcessName) { continue }
    return $window
  }
  throw "No window found with title containing '$WindowTitleContains' hosted by process '$ProcessName'. Make sure the mini program is already open."
}

function Get-WindowRect([IntPtr]$handle) {
  $rect = New-Object MiniProxy.Win32+RECT
  [MiniProxy.Win32]::GetWindowRect($handle, [ref]$rect) | Out-Null
  $width = $rect.Right - $rect.Left
  $height = $rect.Bottom - $rect.Top
  if ($width -le 0 -or $height -le 0) {
    throw "Window has no usable size (is it minimized or offscreen?)."
  }
  [pscustomobject]@{ Left = $rect.Left; Top = $rect.Top; Width = $width; Height = $height }
}

function Click-Ratio($rect, [double]$xRatio, [double]$yRatio, [string]$label) {
  $x = [int]($rect.Left + ($rect.Width * $xRatio))
  $y = [int]($rect.Top + ($rect.Height * $yRatio))
  [MiniProxy.Win32]::SetCursorPos($x, $y) | Out-Null
  Start-Sleep -Milliseconds 200
  [MiniProxy.Win32]::mouse_event(0x0002, 0, 0, 0, 0)  # left button down
  [MiniProxy.Win32]::mouse_event(0x0004, 0, 0, 0, 0)  # left button up
  Write-Output "Clicked $label at ($x, $y)"
}

# Step 1: bring the mini program window to the foreground.
$target = Find-MiniProgramWindow
$handle = [IntPtr]$target.Current.NativeWindowHandle
[MiniProxy.Win32]::ShowWindow($handle, 9) | Out-Null   # SW_RESTORE
[MiniProxy.Win32]::SetForegroundWindow($handle) | Out-Null
Start-Sleep -Milliseconds 500

for ($cycle = 1; $cycle -le $RepeatCount; $cycle++) {
  Write-Output "--- cycle $cycle of $RepeatCount ---"
  $rect = Get-WindowRect $handle

  # Step 2: open the cart tab from the bottom nav.
  Click-Ratio $rect $CartTabXRatio $CartTabYRatio "cart tab"
  Start-Sleep -Milliseconds 500

  # Step 3: click the "全部" sub-tab.
  $rect = Get-WindowRect $handle
  Click-Ratio $rect $AllTabXRatio $AllTabYRatio "all tab"

  # Step 4: wait, then click the "服务" sub-tab.
  Start-Sleep -Seconds $FirstDelaySeconds
  $rect = Get-WindowRect $handle
  Click-Ratio $rect $ServiceTabXRatio $ServiceTabYRatio "service tab"

  # Step 5: wait, then click the "全部" sub-tab again.
  Start-Sleep -Seconds $SecondDelaySeconds
  $rect = Get-WindowRect $handle
  Click-Ratio $rect $AllTabXRatio $AllTabYRatio "all tab"
}
