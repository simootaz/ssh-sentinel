// Package migrations embeds the numbered SQL files applied in order by
// "server migrate" and by the container on start. Files are named
// NNN_description.sql and applied once each, tracked in schema_migrations.
package migrations

import "embed"

// FS holds every *.sql file of this folder.
//
//go:embed *.sql
var FS embed.FS
