package uiauto

import "fmt"

const (
	InputModeForeground = "foreground"
	InputModeBackground = "background"
)

// CoordCycleOptions describes a coordinate-click automation cycle. Click
// positions are ratios of the current target window dimensions.
type CoordCycleOptions struct {
	ProcessName         string  `json:"processName"`
	WindowTitleContains string  `json:"windowTitleContains"`
	InputMode           string  `json:"inputMode,omitempty"`
	RepeatCount         int     `json:"repeatCount"`
	CartTabXRatio       float64 `json:"cartTabXRatio"`
	CartTabYRatio       float64 `json:"cartTabYRatio"`
	AllTabXRatio        float64 `json:"allTabXRatio"`
	AllTabYRatio        float64 `json:"allTabYRatio"`
	ServiceTabXRatio    float64 `json:"serviceTabXRatio"`
	ServiceTabYRatio    float64 `json:"serviceTabYRatio"`
	FirstDelaySeconds   int     `json:"firstDelaySeconds"`
}

type BackgroundProbeOptions struct {
	ProcessName         string  `json:"processName"`
	WindowTitleContains string  `json:"windowTitleContains"`
	XRatio              float64 `json:"xRatio"`
	YRatio              float64 `json:"yRatio"`
	CandidateIndex      int     `json:"candidateIndex"`
}

type ProbePoint struct {
	X int `json:"x"`
	Y int `json:"y"`
}

type ProbeRect struct {
	Left   int32 `json:"left"`
	Top    int32 `json:"top"`
	Right  int32 `json:"right"`
	Bottom int32 `json:"bottom"`
}

type BackgroundClickCandidate struct {
	Handle      string     `json:"handle"`
	ClassName   string     `json:"className"`
	ProcessID   uint32     `json:"processId"`
	Depth       int        `json:"depth"`
	Rect        ProbeRect  `json:"rect"`
	ClientPoint ProbePoint `json:"clientPoint"`
}

type BackgroundMessageResult struct {
	Message string `json:"message"`
	Sent    bool   `json:"sent"`
	Error   string `json:"error,omitempty"`
}

type BackgroundProbeResult struct {
	WindowTitle      string                     `json:"windowTitle"`
	WindowHandle     string                     `json:"windowHandle"`
	ScreenPoint      ProbePoint                 `json:"screenPoint"`
	SelectedIndex    int                        `json:"selectedIndex"`
	Selected         BackgroundClickCandidate   `json:"selected"`
	Candidates       []BackgroundClickCandidate `json:"candidates"`
	Messages         []BackgroundMessageResult  `json:"messages"`
	ForegroundBefore string                     `json:"foregroundBefore"`
	ForegroundAfter  string                     `json:"foregroundAfter"`
	CursorBefore     ProbePoint                 `json:"cursorBefore"`
	CursorAfter      ProbePoint                 `json:"cursorAfter"`
}

func (options BackgroundProbeOptions) withDefaults() BackgroundProbeOptions {
	if options.ProcessName == "" {
		options.ProcessName = "WeChatAppEx"
	}
	if options.WindowTitleContains == "" {
		options.WindowTitleContains = "京东"
	}
	return options
}

func ValidateBackgroundProbeOptions(options BackgroundProbeOptions) error {
	if options.XRatio <= 0 || options.XRatio > 1 {
		return fmt.Errorf("xRatio must be greater than 0 and at most 1")
	}
	if options.YRatio <= 0 || options.YRatio > 1 {
		return fmt.Errorf("yRatio must be greater than 0 and at most 1")
	}
	if options.CandidateIndex < 0 {
		return fmt.Errorf("candidateIndex cannot be negative")
	}
	return nil
}

func (options CoordCycleOptions) withDefaults() CoordCycleOptions {
	if options.ProcessName == "" {
		options.ProcessName = "WeChatAppEx"
	}
	if options.WindowTitleContains == "" {
		options.WindowTitleContains = "京东"
	}
	if options.InputMode == "" {
		options.InputMode = InputModeForeground
	}
	if options.CartTabXRatio == 0 {
		options.CartTabXRatio = 0.70
	}
	if options.CartTabYRatio == 0 {
		options.CartTabYRatio = 0.95
	}
	if options.AllTabXRatio == 0 {
		options.AllTabXRatio = 0.10
	}
	if options.AllTabYRatio == 0 {
		options.AllTabYRatio = 0.108
	}
	if options.ServiceTabXRatio == 0 {
		options.ServiceTabXRatio = 0.62
	}
	if options.ServiceTabYRatio == 0 {
		options.ServiceTabYRatio = 0.108
	}
	return options
}

// ValidateCoordCycleOptions rejects values that could click outside the target
// window or create surprising unbounded behavior. RepeatCount == 0 means an
// intentional infinite run and FirstDelaySeconds == 0 means no delay.
func ValidateCoordCycleOptions(options CoordCycleOptions) error {
	if options.InputMode != InputModeForeground && options.InputMode != InputModeBackground {
		return fmt.Errorf("inputMode must be %q or %q", InputModeForeground, InputModeBackground)
	}
	if options.RepeatCount < 0 {
		return fmt.Errorf("repeatCount cannot be negative; use 0 for an infinite run")
	}
	if options.FirstDelaySeconds < 0 || options.FirstDelaySeconds > 3600 {
		return fmt.Errorf("firstDelaySeconds must be between 0 and 3600")
	}
	for name, ratio := range map[string]float64{
		"cartTabXRatio":    options.CartTabXRatio,
		"cartTabYRatio":    options.CartTabYRatio,
		"allTabXRatio":     options.AllTabXRatio,
		"allTabYRatio":     options.AllTabYRatio,
		"serviceTabXRatio": options.ServiceTabXRatio,
		"serviceTabYRatio": options.ServiceTabYRatio,
	} {
		if ratio <= 0 || ratio > 1 {
			return fmt.Errorf("%s must be greater than 0 and at most 1", name)
		}
	}
	return nil
}
