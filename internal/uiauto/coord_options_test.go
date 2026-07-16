package uiauto

import "testing"

func TestCoordCycleOptionsPreserveZeroDelay(t *testing.T) {
	options := (CoordCycleOptions{}).withDefaults()
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
