package providers

import "errors"

// IsUnknown reports whether err is (or wraps) ErrUnknownProvider.
func IsUnknown(err error) bool { return errors.Is(err, ErrUnknownProvider) }

// ReasonOf extracts a typed discovery failure reason; empty when err is
// not a discovery Failure.
func ReasonOf(err error) FailureReason {
	var failure *Failure
	if errors.As(err, &failure) {
		return failure.Reason
	}
	return ""
}
