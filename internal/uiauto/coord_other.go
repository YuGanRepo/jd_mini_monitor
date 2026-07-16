//go:build !windows

package uiauto

import (
	"context"
	"fmt"
	"log"
)

// RunCoordCycle is unsupported outside Windows.
func RunCoordCycle(_ context.Context, options CoordCycleOptions, _ *log.Logger, _ func(cycle int)) error {
	if err := ValidateCoordCycleOptions(options.withDefaults()); err != nil {
		return err
	}
	return fmt.Errorf("coordinate UI automation is only supported on Windows")
}

// CheckWindowAvailable is unsupported outside Windows.
func CheckWindowAvailable(options CoordCycleOptions) error {
	if err := ValidateCoordCycleOptions(options.withDefaults()); err != nil {
		return err
	}
	return fmt.Errorf("coordinate UI automation is only supported on Windows")
}

// ProbeBackgroundClick is unsupported outside Windows.
func ProbeBackgroundClick(options BackgroundProbeOptions) (BackgroundProbeResult, error) {
	if err := ValidateBackgroundProbeOptions(options.withDefaults()); err != nil {
		return BackgroundProbeResult{}, err
	}
	return BackgroundProbeResult{}, fmt.Errorf("background UI automation is only supported on Windows")
}
