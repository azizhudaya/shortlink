package model

import "time"

// Link mirrors the links table. UserID and the Disabled* fields are unused
// for now (no accounts or takedown UI yet) but exist so adding them later
// needs no schema change.
type Link struct {
	ShortCode     string
	LongURL       string
	IsCustom      bool
	UserID        *int64
	DisabledAt    *time.Time
	DisabledBy    *int64
	DisableReason *string
	CreatedAt     time.Time
}

// IsDisabled reports whether the redirect handler should return 410
// instead of 302. Only a manual SQL takedown sets DisabledAt for now.
func (l Link) IsDisabled() bool {
	return l.DisabledAt != nil
}
