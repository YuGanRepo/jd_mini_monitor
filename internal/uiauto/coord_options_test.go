package uiauto

import "testing"

func TestCoordCycleOptionsPreserveZeroDelay(t *testing.T) {
	options := (CoordCycleOptions{}).withDefaults()
	if options.InputMode != InputModeForeground {
		t.Fatalf("InputMode = %q, want %q", options.InputMode, InputModeForeground)
	}
	if options.FirstDelaySeconds != 0 {
		t.Fatalf("FirstDelaySeconds = %d, want 0", options.FirstDelaySeconds)
	}
	if err := ValidateCoordCycleOptions(options); err != nil {
		t.Fatalf("ValidateCoordCycleOptions() error = %v", err)
	}
}

func TestValidateCoordCycleOptionsRejectsUnsafeValues(t *testing.T) {
	base := (CoordCycleOptions{}).withDefaults()
	tests := []struct {
		name   string
		mutate func(*CoordCycleOptions)
	}{
		{name: "negative repeat", mutate: func(options *CoordCycleOptions) { options.RepeatCount = -1 }},
		{name: "negative delay", mutate: func(options *CoordCycleOptions) { options.FirstDelaySeconds = -1 }},
		{name: "ratio above one", mutate: func(options *CoordCycleOptions) { options.CartTabXRatio = 1.1 }},
		{name: "negative ratio", mutate: func(options *CoordCycleOptions) { options.ServiceTabYRatio = -0.1 }},
		{name: "unknown input mode", mutate: func(options *CoordCycleOptions) { options.InputMode = "automatic" }},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			options := base
			test.mutate(&options)
			if err := ValidateCoordCycleOptions(options); err == nil {
				t.Fatal("ValidateCoordCycleOptions() error = nil")
			}
		})
	}
}

func TestValidateCoordCycleOptionsAllowsBackgroundInput(t *testing.T) {
	options := (CoordCycleOptions{InputMode: InputModeBackground}).withDefaults()
	if err := ValidateCoordCycleOptions(options); err != nil {
		t.Fatalf("ValidateCoordCycleOptions() error = %v", err)
	}
}

func TestBackgroundProbeOptionsDefaults(t *testing.T) {
	options := (BackgroundProbeOptions{XRatio: 0.1, YRatio: 0.2}).withDefaults()
	if options.ProcessName != "WeChatAppEx" {
		t.Fatalf("ProcessName = %q, want WeChatAppEx", options.ProcessName)
	}
	if options.WindowTitleContains != "京东" {
		t.Fatalf("WindowTitleContains = %q, want 京东", options.WindowTitleContains)
	}
	if err := ValidateBackgroundProbeOptions(options); err != nil {
		t.Fatalf("ValidateBackgroundProbeOptions() error = %v", err)
	}
}

func TestValidateBackgroundProbeOptionsRejectsUnsafeValues(t *testing.T) {
	tests := []BackgroundProbeOptions{
		{XRatio: 0, YRatio: 0.2},
		{XRatio: 1.1, YRatio: 0.2},
		{XRatio: 0.1, YRatio: 0},
		{XRatio: 0.1, YRatio: 1.1},
		{XRatio: 0.1, YRatio: 0.2, CandidateIndex: -1},
	}
	for _, options := range tests {
		if err := ValidateBackgroundProbeOptions(options); err == nil {
			t.Fatalf("ValidateBackgroundProbeOptions(%+v) error = nil", options)
		}
	}
}
