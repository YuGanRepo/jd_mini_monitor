//go:build !windows

package uiauto

import (
	"context"
	"fmt"
	"log"
)

// CoordCycleOptions mirrors the Windows implementation's shape so callers on any
// platform can compile against the same type.
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
}

// RunCoordCycle is unsupported outside Windows.
func RunCoordCycle(_ context.Context, _ CoordCycleOptions, _ *log.Logger, _ func(cycle int)) error {
	return fmt.Errorf("coordinate UI automation is only supported on Windows")
}

// CheckWindowAvailable is unsupported outside Windows.
func CheckWindowAvailable(_ CoordCycleOptions) error {
	return fmt.Errorf("coordinate UI automation is only supported on Windows")
}
