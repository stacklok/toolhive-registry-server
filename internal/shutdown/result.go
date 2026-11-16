// Package shutdown normalizes cancellation-only lifecycle results.
package shutdown

import (
	"context"
	"errors"
)

// Result treats only cancellation-only errors as clean shutdown. Joined or
// wrapped backend failures remain available to the caller through errors.Is.
func Result(err error) error {
	if err == nil || err == context.Canceled || err == context.DeadlineExceeded {
		return nil
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		var result error
		for _, child := range joined.Unwrap() {
			result = errors.Join(result, Result(child))
		}
		return result
	}
	if wrapped, ok := err.(interface{ Unwrap() error }); ok {
		if child := wrapped.Unwrap(); child != nil && Result(child) == nil {
			return nil
		}
	}
	return err
}
