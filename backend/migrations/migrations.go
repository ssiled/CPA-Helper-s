package migrations

import "embed"

// FS contains SQL migrations embedded into the application binary.
//
//go:embed *.sql
var FS embed.FS
