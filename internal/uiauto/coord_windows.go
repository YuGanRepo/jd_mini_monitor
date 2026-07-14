//go:build windows

package uiauto

import (
	"context"
	"fmt"
	"log"
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
	procOpenProcess                = kernel32.NewProc("OpenProcess")
	procCloseHandle                = kernel32.NewProc("CloseHandle")
	procQueryFullProcessImageNameW = kernel32.NewProc("QueryFullProcessImageNameW")
)

const (
	swRestore               = 9
	swShow                  = 5
	mouseEventFLeftDown     = 0x0002
	mouseEventFLeftUp       = 0x0004
	processQueryLimitedInfo = 0x1000
)

type windowRect struct {
	Left, Top, Right, Bottom int32
}

type foundWindow struct {
	handle uintptr
	title  string
	pid    uint32
}

// CoordCycleOptions describes a coordinate-click automation cycle against a target
// window found by title substring and host process name. Every click position is
// expressed as a ratio (0-1) of the target window's current width/height, so it
// keeps working if the window is moved or resized.
type CoordCycleOptions struct {
	ProcessName         string  `json:"processName"`
	WindowTitleContains string  `json:"windowTitleContains"`
	RepeatCount         int     `json:"repeatCount"`
	CartTabXRatio       float64 `json:"cartTabXRatio"`
	CartTabYRatio       float64 `json:"cartTabYRatio"`
	AllTabXRatio        float64 `json:"allTabXRatio"`
	AllTabYRatio        float64 `json:"allTabYRatio"`
	ServiceTabXRatio    float64 `json:"serviceTabXRatio"`
	ServiceTabYRatio    float64 `json:"serviceTabYRatio"`
	FirstDelaySeconds   int     `json:"firstDelaySeconds"`
	SecondDelaySeconds  int     `json:"secondDelaySeconds"`
}

func (options CoordCycleOptions) withDefaults() CoordCycleOptions {
	if options.ProcessName == "" {
		options.ProcessName = "WeChatAppEx"
	}
	if options.WindowTitleContains == "" {
		options.WindowTitleContains = "京东"
	}
	if options.RepeatCount <= 0 {
		options.RepeatCount = 1
	}
	if options.CartTabXRatio <= 0 {
		options.CartTabXRatio = 0.70
	}
	if options.CartTabYRatio <= 0 {
		options.CartTabYRatio = 0.95
	}
	if options.AllTabXRatio <= 0 {
		options.AllTabXRatio = 0.10
	}
	if options.AllTabYRatio <= 0 {
		options.AllTabYRatio = 0.108
	}
	if options.ServiceTabXRatio <= 0 {
		options.ServiceTabXRatio = 0.62
	}
	if options.ServiceTabYRatio <= 0 {
		options.ServiceTabYRatio = 0.108
	}
	if options.FirstDelaySeconds <= 0 {
		options.FirstDelaySeconds = 30
	}
	if options.SecondDelaySeconds <= 0 {
		options.SecondDelaySeconds = 5
	}
	return options
}

// RunCoordCycle brings the target mini program window to the foreground, then repeats:
// click the cart tab, click "全部", wait FirstDelaySeconds, click "服务",
// wait SecondDelaySeconds, click "全部" again. onCycle, if non-nil, is called with the
// 1-based cycle index before each cycle starts. The context can be cancelled to stop
// between clicks/waits.
func RunCoordCycle(ctx context.Context, options CoordCycleOptions, logger *log.Logger, onCycle func(cycle int)) error {
	options = options.withDefaults()

	window, err := findWindow(options.WindowTitleContains, options.ProcessName)
	if err != nil {
		return err
	}

	logf := func(format string, args ...any) {
		if logger != nil {
			logger.Printf(format, args...)
		}
	}

	// Bring the window forward once up front; every click re-asserts foreground
	// (ensureForeground) in case the user switched windows during a wait.
	if err := ensureForeground(ctx, window.handle, logf); err != nil {
		return err
	}
	if err := sleepCtx(ctx, 500*time.Millisecond); err != nil {
		return err
	}

	for cycle := 1; cycle <= options.RepeatCount; cycle++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		if onCycle != nil {
			onCycle(cycle)
		}
		logf("jd cart cycle %d/%d starting on window %q", cycle, options.RepeatCount, window.title)

		if err := clickAndLog(ctx, window.handle, options.CartTabXRatio, options.CartTabYRatio, "cart tab", logf); err != nil {
			return err
		}
		if err := sleepCtx(ctx, 500*time.Millisecond); err != nil {
			return err
		}

		if err := clickAndLog(ctx, window.handle, options.AllTabXRatio, options.AllTabYRatio, "all tab", logf); err != nil {
			return err
		}
		if err := sleepCtx(ctx, time.Duration(options.FirstDelaySeconds)*time.Second); err != nil {
			return err
		}

		if err := clickAndLog(ctx, window.handle, options.ServiceTabXRatio, options.ServiceTabYRatio, "service tab", logf); err != nil {
			return err
		}
		if err := sleepCtx(ctx, time.Duration(options.SecondDelaySeconds)*time.Second); err != nil {
			return err
		}

		if err := clickAndLog(ctx, window.handle, options.AllTabXRatio, options.AllTabYRatio, "all tab", logf); err != nil {
			return err
		}

		logf("jd cart cycle %d/%d finished", cycle, options.RepeatCount)
	}
	return nil
}

// CheckWindowAvailable verifies the target mini-program window is currently open
// (found by title substring + host process). It returns a helpful error when the
// window is not found, so callers can prompt the user to open it before starting
// automation instead of failing partway through.
func CheckWindowAvailable(options CoordCycleOptions) error {
	options = options.withDefaults()
	_, err := findWindow(options.WindowTitleContains, options.ProcessName)
	return err
}

func clickAndLog(ctx context.Context, handle uintptr, xRatio, yRatio float64, label string, logf func(format string, args ...any)) error {
	// Re-assert foreground before every click: coordinate clicks land on whatever
	// window is on top, so the target must be active first.
	if err := ensureForeground(ctx, handle, logf); err != nil {
		return fmt.Errorf("focus window before %s failed: %w", label, err)
	}
	x, y, err := clickRatio(handle, xRatio, yRatio)
	if err != nil {
		return fmt.Errorf("click %s failed: %w", label, err)
	}
	logf("clicked %s at (%d, %d)", label, x, y)
	return nil
}

// ensureForeground makes the target window the active foreground window before a
// click. Windows blocks background processes from stealing focus, so it uses the
// AttachThreadInput technique (attach to the current foreground thread's input
// queue) to reliably call SetForegroundWindow, then verifies and retries.
// It returns an error if the window cannot be activated (e.g. minimized to tray,
// on another virtual desktop, or a modal system dialog is blocking focus).
func ensureForeground(ctx context.Context, handle uintptr, logf func(format string, args ...any)) error {
	const maxAttempts = 5
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

		foreground, _, _ := procGetForegroundWindow.Call()
		var targetPid, foregroundPid uint32
		targetThread, _, _ := procGetWindowThreadProcessId.Call(handle, uintptr(unsafe.Pointer(&targetPid)))
		foregroundThread, _, _ := procGetWindowThreadProcessId.Call(foreground, uintptr(unsafe.Pointer(&foregroundPid)))

		attached := false
		if foregroundThread != 0 && foregroundThread != targetThread {
			if r, _, _ := procAttachThreadInput.Call(foregroundThread, targetThread, 1); r != 0 {
				attached = true
			}
		}
		procBringWindowToTop.Call(handle)
		procSetForegroundWindow.Call(handle)
		procShowWindow.Call(handle, swShow)
		if attached {
			procAttachThreadInput.Call(foregroundThread, targetThread, 0)
		}

		if err := sleepCtx(ctx, 250*time.Millisecond); err != nil {
			return err
		}
		if current, _, _ := procGetForegroundWindow.Call(); current == handle {
			return nil
		}
		logf("ensureForeground: attempt %d/%d could not activate target window %#x", attempt, maxAttempts, handle)
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

		result = foundWindow{handle: hwnd, title: title, pid: pid}
		found = true
		return 0
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
