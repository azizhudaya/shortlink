package alias

// Reserved is the single source of truth for path segments a custom alias
// may not claim. If this list and the router drift, an alias could shadow a
// real route. Future routes (`/login`, `/register`, ...) are reserved now so
// no alias occupies them first.
//
// Generated base62 codes cannot collide with these by construction (every
// entry either contains a non-base62 character or is not exactly 7 chars of
// base62 that Generate would produce with meaningful probability), so this
// check applies to custom aliases only.
var Reserved = map[string]struct{}{
	"api":      {},
	"app":      {},
	"admin":    {},
	"login":    {},
	"logout":   {},
	"signup":   {},
	"register": {},
	"auth":     {},
	"healthz":  {},
	"status":   {},
	"static":   {},
	"assets":   {},

	"_next":       {},
	"favicon.ico": {},
	"robots.txt":  {},
	"sitemap.xml": {},
	".well-known": {},
}

// IsReserved reports whether an alias is on the reserved list. Comparison is
// exact and case-sensitive, like short codes.
func IsReserved(s string) bool {
	_, ok := Reserved[s]
	return ok
}
