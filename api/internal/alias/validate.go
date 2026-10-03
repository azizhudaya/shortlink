// Package alias validates operator-chosen custom aliases.
package alias

import (
	"errors"
	"fmt"
	"regexp"
)

const (
	MinLength = 3
	MaxLength = 32
)

var format = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// Distinct errors so the 400 response can name the specific violation.
var (
	ErrTooShort   = errors.New("alias must be at least 3 characters")
	ErrTooLong    = errors.New("alias must be at most 32 characters")
	ErrBadFormat  = errors.New("alias may contain only letters, digits, underscore, and hyphen")
	ErrIsReserved = errors.New("alias is reserved")
)

// Validate checks an alias against the format rule and the reserved list.
// Whether it is already taken is checked by the store (409).
func Validate(s string) error {
	switch {
	case len(s) < MinLength:
		return fmt.Errorf("%w (got %d)", ErrTooShort, len(s))
	case len(s) > MaxLength:
		return fmt.Errorf("%w (got %d)", ErrTooLong, len(s))
	case !format.MatchString(s):
		return ErrBadFormat
	case IsReserved(s):
		return fmt.Errorf("%w: %q", ErrIsReserved, s)
	}
	return nil
}
