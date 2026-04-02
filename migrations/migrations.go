// Package migrations embeds the SQL migration files into the compiled binary.
//
// Usage from other packages:
//
//	import "github.com/germanamz/tusk/migrations"
//	store, err := sqlite.New(":memory:", migrations.FS)
//
// The go:embed directive below tells the Go compiler to read every .sql file
// in this directory at build time and store their contents in the FS variable.
// At runtime, FS behaves like a read-only filesystem containing those files.
package migrations

import "embed"

// FS contains all *.sql files from the migrations directory, embedded at compile time.
// It implements the fs.FS interface, which means you can use standard fs package
// functions like fs.Glob(), fs.ReadFile(), etc. to access the files.
//
//go:embed *.sql
var FS embed.FS
