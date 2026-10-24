package model

import (
	"strings"
	"time"
)

// Link mirrors the links table.
//
// DisabledBy names who disabled the link (D5): "user:<id>" is the owner,
// "operator:<name>" a takedown, "system:<name>" an automatic takedown. NULL
// with DisabledAt set is an M1-era SQL takedown.
type Link struct {
	ShortCode     string
	LongURL       string
	IsCustom      bool
	UserID        *int64
	DisabledAt    *time.Time
	DisabledBy    *string
	DisableReason *string
	CreatedAt     time.Time
}

// IsDisabled reports whether the redirect handler should return 410
// instead of 302, whoever disabled it.
func (l Link) IsDisabled() bool {
	return l.DisabledAt != nil
}

// Status is the owner-facing state: "active", "disabled" (by the owner, who
// may re-enable it) or "taken_down" (by anyone else, final for the owner).
func (l Link) Status() string {
	switch {
	case l.DisabledAt == nil:
		return "active"
	case l.DisabledBy != nil && strings.HasPrefix(*l.DisabledBy, "user:"):
		return "disabled"
	default:
		return "taken_down"
	}
}
