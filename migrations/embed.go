// Package migrations embeds Phenk's SQL migrations so a single binary can
// bring its own schema. Files are applied in lexical order, which is why they
// are numbered.
package migrations

import "embed"

// FS holds every migration file.
//
//go:embed *.sql
var FS embed.FS
