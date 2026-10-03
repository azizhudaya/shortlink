// Package migrations embeds the SQL migration files into the binary.
//
// The SQL lives here rather than at the repository root because go:embed
// cannot reference parent directories — the directive must sit in a package
// at or above the files it embeds. Keeping the embed declaration next to the
// SQL also means there is exactly one place that knows where migrations are.
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
