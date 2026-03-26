//go:build (darwin && (amd64 || arm64)) || (freebsd && (amd64 || arm64)) || (linux && (386 || amd64 || arm || arm64 || loong64 || ppc64le || riscv64 || s390x)) || (windows && (386 || amd64 || arm64))

package dbopen

import (
	"database/sql"
	"fmt"
	"net/url"

	_ "github.com/glebarez/go-sqlite" // register "sqlite" database/sql driver
)

// Open opens a SQLite database at dbPath using the glebarez/go-sqlite driver
// with standard Crucible pragmas applied.
func Open(dbPath string) (*sql.DB, error) {
	params := url.Values{}
	for name, value := range Pragmas {
		params.Add("_pragma", fmt.Sprintf("%s(%s)", name, value))
	}

	dsn := fmt.Sprintf("file:%s?%s", dbPath, params.Encode())
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	return db, nil
}
