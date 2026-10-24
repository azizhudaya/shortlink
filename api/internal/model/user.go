package model

import "time"

// User is a Google-bound account (D3). Email is a display copy refreshed on
// every sign-in; GoogleSub is the identity.
type User struct {
	ID            int64
	GoogleSub     string
	Email         string
	IsActive      bool
	IsWhitelisted bool // D29: no daily creation quota
	CreatedAt     time.Time
}

// Session is what a valid session cookie resolves to.
type Session struct {
	UserID    int64
	Email     string
	ExpiresAt time.Time
}
