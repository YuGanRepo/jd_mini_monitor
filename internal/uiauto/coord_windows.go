//go:build windows

package uiauto

import (
	"context"
	"fmt"
	"log"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

var (
	user32   = syscall.NewLazyDLL("user32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")

	procEnumWindows                = user32.NewProc("EnumWindows")
	procGetWindowTextW             = user32.NewProc("GetWindowTextW")
	procGetWindowTextLengthW       = user32.NewProc("GetWindowTextLengthW")
	procIsWindowVisible            = user32.NewProc("IsWindowVisible")
	procGetWindowThreadProcessId   = user32.NewProc("GetWindowThreadProcessId")
	procGetWindowRect              = user32.NewProc("GetWindowRect")
	procSetForegroundWindow        = user32.NewProc("SetForegroundWindow")
	procGetForegroundWindow        = user32.NewProc("GetForegroundWindow")
	procAttachThreadInput          = user32.NewProc("AttachThreadInput")
	procBringWindowToTop           = user32.NewProc("BringWindowToTop")
	procIsIconic                   = user32.NewProc("IsIconic")
	procShowWindow                 = user32.NewProc("ShowWindow")
	procSetCursorPos               = user32.NewProc("SetCursorPos")
	procMouseEvent                 = user32.NewProc("mouse_event")
	procKeybdEvent                 = user32.NewProc("keybd_event")
	procSystemParametersInfoW      = user32.NewProc("SystemParametersInfoW")
	procSetWindowPos               = user32.NewProc("SetWindowPos")
	procEnumChildWindows           = user32.NewProc("EnumChildWindows")
	procGetClassNameW              = user32.NewProc("GetClassNameW")
	procGetParent                  = user32.NewProc("GetParent")
	procScreenToClient             = user32.NewProc("ScreenToClient")
	procGetCursorPos               = user32.NewProc("GetCursorPos")
	procSendMessageTimeoutW        = user32.NewProc("SendMessageTimeoutW")
	procOpenProcess                = kernel32.NewProc("OpenProcess")
	procCloseHandle                = kernel32.NewProc("CloseHandle")
	procGetCurrentThreadId         = kernel32.NewProc("GetCurrentThreadId")
	procQueryFullProcessImageNameW = kernel32.NewProc("QueryFullProcessImageNameW")
)

const (
	swRestore               = 9
	swShow                  = 5
	mouseEventFLeftDown     = 0x0002
	mouseEventFLeftUp       = 0x0004
	processQueryLimitedInfo = 0x1000
	// serviceToAllFixedDelaySeconds is the fixed wait after clicking 服务 before
	// switching back to 全部. This part of the flow is not user-configurable.
	serviceToAllFixedDelaySeconds = 5
	vkMenu                        = 0x12 // ALT
	keyEventFKeyUp                = 0x0002
	spiSetForegroundLockTimeout   = 0x2001
	spifSendChange                = 0x0002
	swpNoSize                     = 0x0001
	swpNoMove                     = 0x0002
	swpShowWindow                 = 0x0040
	wmMouseMove                   = 0x0200
	wmLButtonDown                 = 0x0201
	wmLButtonUp                   = 0x0202
	mkLButton                     = 0x0001
	smtoAbortIfHung               = 0x0002
)

// HWND_TOPMOST (-1) / HWND_NOTOPMOST (-2) as uintptr values.
var (
	hwndTopmost   = ^uintptr(0) // -1
	hwndNotopmost = ^uintptr(1) // -2
)

type windowRect struct {
	Left, Top, Right, Bottom int32
}

type foundWindow struct {
	handle uintptr
	title  string
	pid    uint32
}

type nativePoint struct {
	X, Y int32
}

type backgroundCandidate struct {
	handle      uintptr
	className   string
	pid         uint32
	depth       int
	rect        windowRect
	clientPoint nativePoint
}

// ProbeBackgroundClick sends one click directly to a target window descendant.
// It never activates the target window and never moves or clicks the system cursor.
// A successful message delivery only proves that Windows accepted the messages;
// callers must verify the target application actually changed state.
func ProbeBackgroundClick(options BackgroundProbeOptions) (BackgroundProbeResult, error) {
	options = options.withDefaults()
	if err := ValidateBackgroundProbeOptions(options); err != nil {
		return BackgroundProbeResult{}, err
	}

	window, err := findWindow(options.WindowTitleContains, options.ProcessName)
	if err != nil {
		return BackgroundProbeResult{}, err
	}
	if iconic, _, _ := procIsIconic.Call(window.handle); iconic != 0 {
		return BackgroundProbeResult{}, fmt.Errorf("target window is minimized; restore it before using background input")
	}

	point, candidates, err := findBackgroundCandidates(window.handle, window.pid, options.XRatio, options.YRatio)
	if err != nil {
		return BackgroundProbeResult{}, err
	}
	if options.CandidateIndex >= len(candidates) {
		return BackgroundProbeResult{}, fmt.Errorf("candidateIndex %d is out of range; found %d candidates", options.CandidateIndex, len(candidates))
	}

	foregroundBefore, _, _ := procGetForegroundWindow.Call()
	cursorBefore := getCursorPosition()
	selected := candidates[options.CandidateIndex]
	messages := sendBackgroundClick(selected.handle, selected.clientPoint)
	foregroundAfter, _, _ := procGetForegroundWindow.Call()
	cursorAfter := getCursorPosition()

	result := BackgroundProbeResult{
		WindowTitle:      window.title,
		WindowHandle:     formatHandle(window.handle),
		ScreenPoint:      ProbePoint{X: int(point.X), Y: int(point.Y)},
		SelectedIndex:    options.CandidateIndex,
		Selected:         exportBackgroundCandidate(selected),
		Candidates:       make([]BackgroundClickCandidate, 0, len(candidates)),
		Messages:         messages,
		ForegroundBefore: formatHandle(foregroundBefore),
		ForegroundAfter:  formatHandle(foregroundAfter),
		CursorBefore:     ProbePoint{X: int(cursorBefore.X), Y: int(cursorBefore.Y)},
		CursorAfter:      ProbePoint{X: int(cursorAfter.X), Y: int(cursorAfter.Y)},
	}
	for _, candidate := range candidates {
		result.Candidates = append(result.Candidates, exportBackgroundCandidate(candidate))
	}
	for _, message := range messages {
		if !message.Sent {
			return result, fmt.Errorf("send %s to %s failed: %s", message.Message, result.Selected.Handle, message.Error)
		}
	}
	return result, nil
}

func findBackgroundCandidates(handle uintptr, targetPID uint32, xRatio, yRatio float64) (nativePoint, []backgroundCandidate, error) {
	var targetRect windowRect
	if ok, _, _ := procGetWindowRect.Call(handle, uintptr(unsafe.Pointer(&targetRect))); ok == 0 {
		return nativePoint{}, nil, fmt.Errorf("get target window rect failed")
	}
	width := targetRect.Right - targetRect.Left
	height := targetRect.Bottom - targetRect.Top
	if width <= 0 || height <= 0 {
		return nativePoint{}, nil, fmt.Errorf("target window has no usable size")
	}
	point := nativePoint{
		X: targetRect.Left + int32(float64(width)*xRatio),
		Y: targetRect.Top + int32(float64(height)*yRatio),
	}

	handles := []uintptr{handle}
	callback := syscall.NewCallback(func(child uintptr, _ uintptr) uintptr {
		handles = append(handles, child)
		return 1
	})
	procEnumChildWindows.Call(handle, callback, 0)

	candidates := make([]backgroundCandidate, 0, len(handles))
	for _, candidateHandle := range handles {
		visible, _, _ := procIsWindowVisible.Call(candidateHandle)
		if visible == 0 {
			continue
		}
		var rect windowRect
		if ok, _, _ := procGetWindowRect.Call(candidateHandle, uintptr(unsafe.Pointer(&rect))); ok == 0 || !rectContains(rect, point) {
			continue
		}
		clientPoint := point
		if ok, _, _ := procScreenToClient.Call(candidateHandle, uintptr(unsafe.Pointer(&clientPoint))); ok == 0 {
			continue
		}
		var pid uint32
		procGetWindowThreadProcessId.Call(candidateHandle, uintptr(unsafe.Pointer(&pid)))
		if pid != targetPID {
			continue
		}
		candidates = append(candidates, backgroundCandidate{
			handle:      candidateHandle,
			className:   windowClassName(candidateHandle),
			pid:         pid,
			depth:       windowDepth(candidateHandle, handle),
			rect:        rect,
			clientPoint: clientPoint,
		})
	}
	if len(candidates) == 0 {
		return point, nil, fmt.Errorf("no visible target window descendant contains the click point")
	}

	sort.SliceStable(candidates, func(i, j int) bool {
		iRenderer := strings.Contains(strings.ToLower(candidates[i].className), "renderwidgethost")
		jRenderer := strings.Contains(strings.ToLower(candidates[j].className), "renderwidgethost")
		if iRenderer != jRenderer {
			return iRenderer
		}
		if candidates[i].depth != candidates[j].depth {
			return candidates[i].depth > candidates[j].depth
		}
		return rectArea(candidates[i].rect) < rectArea(candidates[j].rect)
	})
	return point, candidates, nil
}

func sendBackgroundClick(handle uintptr, point nativePoint) []BackgroundMessageResult {
	lParam := uintptr(uint32(uint16(point.X)) | uint32(uint16(point.Y))<<16)
	return []BackgroundMessageResult{
		sendBackgroundMessage(handle, wmMouseMove, 0, lParam, "WM_MOUSEMOVE"),
		sendBackgroundMessage(handle, wmLButtonDown, mkLButton, lParam, "WM_LBUTTONDOWN"),
		sendBackgroundMessage(handle, wmLButtonUp, 0, lParam, "WM_LBUTTONUP"),
	}
}

func sendBackgroundMessage(handle uintptr, message, wParam, lParam uintptr, name string) BackgroundMessageResult {
	var result uintptr
	ok, _, callErr := procSendMessageTimeoutW.Call(handle, message, wParam, lParam, smtoAbortIfHung, 1000, uintptr(unsafe.Pointer(&result)))
	messageResult := BackgroundMessageResult{Message: name, Sent: ok != 0}
	if ok == 0 {
		messageResult.Error = callErr.Error()
	}
	return messageResult
}

func exportBackgroundCandidate(candidate backgroundCandidate) BackgroundClickCandidate {
	return BackgroundClickCandidate{
		Handle:    formatHandle(candidate.handle),
		ClassName: candidate.className,
		ProcessID: candidate.pid,
		Depth:     candidate.depth,
		Rect: ProbeRect{
			Left: candidate.rect.Left, Top: candidate.rect.Top,
			Right: candidate.rect.Right, Bottom: candidate.rect.Bottom,
		},
		ClientPoint: ProbePoint{X: int(candidate.clientPoint.X), Y: int(candidate.clientPoint.Y)},
	}
}

func windowClassName(handle uintptr) string {
	buffer := make([]uint16, 256)
	length, _, _ := procGetClassNameW.Call(handle, uintptr(unsafe.Pointer(&buffer[0])), uintptr(len(buffer)))
	if length == 0 {
		return ""
	}
	return syscall.UTF16ToString(buffer[:length])
}

func windowDepth(handle, root uintptr) int {
	depth := 0
	for handle != 0 && handle != root {
		handle, _, _ = procGetParent.Call(handle)
		depth++
	}
	return depth
}

func getCursorPosition() nativePoint {
	var point nativePoint
	procGetCursorPos.Call(uintptr(unsafe.Pointer(&point)))
	return point
}

func rectContains(rect windowRect, point nativePoint) bool {
	return point.X >= rect.Left && point.X < rect.Right && point.Y >= rect.Top && point.Y < rect.Bottom
}

func rectArea(rect windowRect) int64 {
	return int64(rect.Right-rect.Left) * int64(rect.Bottom-rect.Top)
}

func formatHandle(handle uintptr) string {
	return fmt.Sprintf("0x%X", handle)
}

// RunCoordCycle makes sure the target mini program is on the cart page (clicks the
// cart tab in the bottom nav once up front), then
// repeats: click the cart tab, click "全部", wait FirstDelaySeconds, click "服务",
// wait a fixed 5s, click "全部" again. When RepeatCount <= 0 the cycle repeats
// indefinitely until the context is cancelled. onCycle, if non-nil, is called with the
// 1-based cycle index before each cycle starts. The context can be cancelled to stop
// between clicks/waits.
func RunCoordCycle(ctx context.Context, options CoordCycleOptions, logger *log.Logger, onCycle func(cycle int)) error {
	options = options.withDefaults()
	if err := ValidateCoordCycleOptions(options); err != nil {
		return err
	}

	window, err := findWindow(options.WindowTitleContains, options.ProcessName)
	if err != nil {
		return err
	}

	logf := func(format string, args ...any) {
		if logger != nil {
			logger.Printf(format, args...)
		}
	}

	if options.InputMode == InputModeForeground {
		// Every foreground click re-asserts focus in case the user switched windows.
		if err := ensureForeground(ctx, window.handle, logf); err != nil {
			return err
		}
	} else if iconic, _, _ := procIsIconic.Call(window.handle); iconic != 0 {
		return fmt.Errorf("target window is minimized; restore it before using background input")
	}
	if err := sleepCtx(ctx, 500*time.Millisecond); err != nil {
		return err
	}

	// Make sure we start on the cart page. We cannot read the mini program's UI tree
	// to detect the current page, so click the cart tab in the bottom nav once up
	// front (harmless if already on the cart page) and give it time to load before
	// the 全部/服务 cycling begins.
	if err := clickAndLog(ctx, window, options.InputMode, options.CartTabXRatio, options.CartTabYRatio, "cart tab (ensure cart page)", logf); err != nil {
		return err
	}
	if err := sleepCtx(ctx, 1500*time.Millisecond); err != nil {
		return err
	}

	totalLabel := strconv.Itoa(options.RepeatCount)
	if options.RepeatCount <= 0 {
		totalLabel = "\u221e"
	}
	for cycle := 1; options.RepeatCount <= 0 || cycle <= options.RepeatCount; cycle++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		if onCycle != nil {
			onCycle(cycle)
		}
		logf("jd cart cycle %d/%s starting on window %q", cycle, totalLabel, window.title)

		if err := clickAndLog(ctx, window, options.InputMode, options.CartTabXRatio, options.CartTabYRatio, "cart tab", logf); err != nil {
			return err
		}
		if err := sleepCtx(ctx, 500*time.Millisecond); err != nil {
			return err
		}

		if err := clickAndLog(ctx, window, options.InputMode, options.AllTabXRatio, options.AllTabYRatio, "all tab", logf); err != nil {
			return err
		}
		if err := sleepCtx(ctx, time.Duration(options.FirstDelaySeconds)*time.Second); err != nil {
			return err
		}

		if err := clickAndLog(ctx, window, options.InputMode, options.ServiceTabXRatio, options.ServiceTabYRatio, "service tab", logf); err != nil {
			return err
		}
		if err := sleepCtx(ctx, serviceToAllFixedDelaySeconds*time.Second); err != nil {
			return err
		}

		if err := clickAndLog(ctx, window, options.InputMode, options.AllTabXRatio, options.AllTabYRatio, "all tab", logf); err != nil {
			return err
		}

		logf("jd cart cycle %d/%s finished", cycle, totalLabel)
	}
	return nil
}

// CheckWindowAvailable verifies the target mini-program window is currently open
// (found by title substring + host process). It returns a helpful error when the
// window is not found, so callers can prompt the user to open it before starting
// automation instead of failing partway through.
func CheckWindowAvailable(options CoordCycleOptions) error {
	options = options.withDefaults()
	if err := ValidateCoordCycleOptions(options); err != nil {
		return err
	}
	window, err := findWindow(options.WindowTitleContains, options.ProcessName)
	if err != nil {
		return err
	}
	if options.InputMode == InputModeBackground {
		if iconic, _, _ := procIsIconic.Call(window.handle); iconic != 0 {
			return fmt.Errorf("target window is minimized; restore it before using background input")
		}
	}
	return nil
}

func clickAndLog(ctx context.Context, window foundWindow, inputMode string, xRatio, yRatio float64, label string, logf func(format string, args ...any)) error {
	if inputMode == InputModeBackground {
		x, y, targetClass, err := clickBackgroundRatio(ctx, window, xRatio, yRatio)
		if err != nil {
			return fmt.Errorf("background click %s failed: %w", label, err)
		}
		logf("background clicked %s at (%d, %d) targetClass=%s", label, x, y, targetClass)
		return nil
	}

	// Re-assert foreground before every click: coordinate clicks land on whatever
	// window is on top, so the target must be active first.
	if err := ensureForeground(ctx, window.handle, logf); err != nil {
		return fmt.Errorf("focus window before %s failed: %w", label, err)
	}
	x, y, err := clickRatio(window.handle, xRatio, yRatio)
	if err != nil {
		return fmt.Errorf("click %s failed: %w", label, err)
	}
	logf("clicked %s at (%d, %d)", label, x, y)
	return nil
}

func clickBackgroundRatio(ctx context.Context, window foundWindow, xRatio, yRatio float64) (int, int, string, error) {
	if err := ctx.Err(); err != nil {
		return 0, 0, "", err
	}
	if iconic, _, _ := procIsIconic.Call(window.handle); iconic != 0 {
		return 0, 0, "", fmt.Errorf("target window is minimized; restore it before using background input")
	}
	point, candidates, err := findBackgroundCandidates(window.handle, window.pid, xRatio, yRatio)
	if err != nil {
		return 0, 0, "", err
	}
	selected := candidates[0]
	for _, message := range sendBackgroundClick(selected.handle, selected.clientPoint) {
		if !message.Sent {
			return 0, 0, selected.className, fmt.Errorf("send %s to %s failed: %s", message.Message, formatHandle(selected.handle), message.Error)
		}
	}
	return int(point.X), int(point.Y), selected.className, nil
}

// ensureForeground makes the target window the active foreground window before a
// click. Windows blocks background processes from stealing focus, so it uses the
// AttachThreadInput technique (attach to the current foreground thread's input
// queue) to reliably call SetForegroundWindow, then verifies and retries.
// It returns an error if the window cannot be activated (e.g. minimized to tray,
// on another virtual desktop, or a modal system dialog is blocking focus).
func ensureForeground(ctx context.Context, handle uintptr, logf func(format string, args ...any)) error {
	// Windows blocks background processes from stealing focus. Lowering the
	// foreground-lock timeout to 0 lets our SetForegroundWindow calls take effect.
	procSystemParametersInfoW.Call(spiSetForegroundLockTimeout, 0, 0, spifSendChange)

	currentThread, _, _ := procGetCurrentThreadId.Call()

	const maxAttempts = 6
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		if current, _, _ := procGetForegroundWindow.Call(); current == handle {
			return nil
		}

		if iconic, _, _ := procIsIconic.Call(handle); iconic != 0 {
			procShowWindow.Call(handle, swRestore)
		}

		// Synthetic ALT tap: Windows only allows a foreground change shortly after
		// keyboard input, so this "unlocks" SetForegroundWindow for our thread.
		procKeybdEvent.Call(vkMenu, 0, 0, 0)
		procKeybdEvent.Call(vkMenu, 0, keyEventFKeyUp, 0)

		foreground, _, _ := procGetForegroundWindow.Call()
		var targetPid, foregroundPid uint32
		targetThread, _, _ := procGetWindowThreadProcessId.Call(handle, uintptr(unsafe.Pointer(&targetPid)))
		foregroundThread, _, _ := procGetWindowThreadProcessId.Call(foreground, uintptr(unsafe.Pointer(&foregroundPid)))

		// Attach both the current foreground thread and our own thread to the
		// target's input queue so the OS lets us change focus and z-order.
		attachedFg := false
		if foregroundThread != 0 && foregroundThread != targetThread {
			if r, _, _ := procAttachThreadInput.Call(foregroundThread, targetThread, 1); r != 0 {
				attachedFg = true
			}
		}
		attachedCur := false
		if currentThread != 0 && currentThread != targetThread && currentThread != foregroundThread {
			if r, _, _ := procAttachThreadInput.Call(currentThread, targetThread, 1); r != 0 {
				attachedCur = true
			}
		}

		procShowWindow.Call(handle, swShow)
		procBringWindowToTop.Call(handle)
		// Briefly force the window above all others, then drop back so it does not
		// stay permanently always-on-top.
		procSetWindowPos.Call(handle, hwndTopmost, 0, 0, 0, 0, swpNoMove|swpNoSize|swpShowWindow)
		procSetWindowPos.Call(handle, hwndNotopmost, 0, 0, 0, 0, swpNoMove|swpNoSize|swpShowWindow)
		procSetForegroundWindow.Call(handle)

		if attachedCur {
			procAttachThreadInput.Call(currentThread, targetThread, 0)
		}
		if attachedFg {
			procAttachThreadInput.Call(foregroundThread, targetThread, 0)
		}

		if err := sleepCtx(ctx, 250*time.Millisecond); err != nil {
			return err
		}
		if current, _, _ := procGetForegroundWindow.Call(); current == handle {
			return nil
		}
		logf("ensureForeground: attempt %d/%d could not activate target window %#x (foreground=%#x)", attempt, maxAttempts, handle, foreground)
	}
	return fmt.Errorf("could not bring the target window to the foreground; make sure it is open, not minimized to tray, and on the current desktop")
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func clickRatio(handle uintptr, xRatio, yRatio float64) (int, int, error) {
	var r windowRect
	ok, _, _ := procGetWindowRect.Call(handle, uintptr(unsafe.Pointer(&r)))
	if ok == 0 {
		return 0, 0, fmt.Errorf("get window rect failed")
	}
	width := int(r.Right - r.Left)
	height := int(r.Bottom - r.Top)
	if width <= 0 || height <= 0 {
		return 0, 0, fmt.Errorf("window has no usable size (is it minimized or offscreen?)")
	}
	x := int(r.Left) + int(float64(width)*xRatio)
	y := int(r.Top) + int(float64(height)*yRatio)

	procSetCursorPos.Call(uintptr(x), uintptr(y))
	time.Sleep(200 * time.Millisecond)
	procMouseEvent.Call(uintptr(mouseEventFLeftDown), 0, 0, 0, 0)
	procMouseEvent.Call(uintptr(mouseEventFLeftUp), 0, 0, 0, 0)
	return x, y, nil
}

func findWindow(titleContains, processName string) (foundWindow, error) {
	var result foundWindow
	var found bool
	var resultMinimized bool

	callback := syscall.NewCallback(func(hwnd uintptr, _ uintptr) uintptr {
		visible, _, _ := procIsWindowVisible.Call(hwnd)
		if visible == 0 {
			return 1
		}
		length, _, _ := procGetWindowTextLengthW.Call(hwnd)
		if length == 0 {
			return 1
		}
		buf := make([]uint16, length+1)
		procGetWindowTextW.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), length+1)
		title := syscall.UTF16ToString(buf)
		if titleContains != "" && !strings.Contains(title, titleContains) {
			return 1
		}

		var pid uint32
		procGetWindowThreadProcessId.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
		if processName != "" {
			name, err := processImageName(pid)
			if err != nil || !strings.EqualFold(name, processName) {
				return 1
			}
		}

		iconic, _, _ := procIsIconic.Call(hwnd)
		minimized := iconic != 0
		if !found || (resultMinimized && !minimized) {
			result = foundWindow{handle: hwnd, title: title, pid: pid}
			found = true
			resultMinimized = minimized
		}
		if !minimized {
			return 0
		}
		return 1
	})

	procEnumWindows.Call(callback, 0)
	if !found {
		return foundWindow{}, fmt.Errorf("no window found with title containing %q hosted by process %q; make sure it is already open", titleContains, processName)
	}
	return result, nil
}

func processImageName(pid uint32) (string, error) {
	handle, _, _ := procOpenProcess.Call(uintptr(processQueryLimitedInfo), 0, uintptr(pid))
	if handle == 0 {
		return "", fmt.Errorf("open process %d failed", pid)
	}
	defer procCloseHandle.Call(handle)

	buf := make([]uint16, 260)
	size := uint32(len(buf))
	ok, _, _ := procQueryFullProcessImageNameW.Call(handle, 0, uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&size)))
	if ok == 0 {
		return "", fmt.Errorf("query image name for process %d failed", pid)
	}
	full := syscall.UTF16ToString(buf[:size])
	name := full
	if idx := strings.LastIndexAny(full, `/\`); idx >= 0 {
		name = full[idx+1:]
	}
	return strings.TrimSuffix(name, ".exe"), nil
}
