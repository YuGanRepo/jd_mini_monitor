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
