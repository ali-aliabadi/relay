package core

import (
	"errors"
	"fmt"
	"time"
)

// ErrInvalid wraps every validation failure; its message is safe to show callers.
var ErrInvalid = errors.New("invalid")

const maxNameLen = 32

// validateName checks client names and recipient usernames: 1-32 characters
// of lowercase letters, digits, '-' and '_', starting with a letter or digit.
func validateName(name string) error {
	if name == "" || len(name) > maxNameLen {
		return fmt.Errorf("%w: name must be 1-%d characters", ErrInvalid, maxNameLen)
	}
	for i, c := range name {
		switch {
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9':
		case (c == '-' || c == '_') && i > 0:
		default:
			return fmt.Errorf("%w: name may only use a-z, 0-9, '-' and '_', starting with a letter or digit", ErrInvalid)
		}
	}
	return nil
}

func validateDisplayName(name string) error {
	if name == "" || len([]rune(name)) > 64 {
		return fmt.Errorf("%w: display name must be 1-64 characters", ErrInvalid)
	}
	return nil
}

func validateTimezone(tz string) error {
	if _, err := time.LoadLocation(tz); err != nil {
		return fmt.Errorf("%w: unknown timezone", ErrInvalid)
	}
	return nil
}
