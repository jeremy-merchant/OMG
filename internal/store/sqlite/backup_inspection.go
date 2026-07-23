package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
)

// BackupInspection is the non-sensitive result of validating a candidate
// SQLite backup for a separately approved, external restore procedure.
type BackupInspection struct {
	Checksum      string `json:"checksum"`
	SchemaVersion int    `json:"schema_version"`
	Integrity     bool   `json:"integrity"`
	Compatible    bool   `json:"compatible"`
}

// InspectBackup validates a backup without opening it for writes. It checks the
// pinned checksum, SQLite integrity, and every applied migration checksum
// against this binary's migration set. It never creates or replaces a store.
func InspectBackup(ctx context.Context, path, expectedChecksum string) (BackupInspection, error) {
	if expectedChecksum == "" || secureStatePath(path, false) != nil {
		return BackupInspection{}, errors.New("sqlite: invalid backup inspection request")
	}
	checksum, err := fileChecksum(path)
	if err != nil {
		return BackupInspection{}, err
	}
	if checksum != expectedChecksum {
		return BackupInspection{}, errors.New("sqlite: backup checksum mismatch")
	}
	healthy, err := integrityPath(ctx, path)
	if err != nil || !healthy {
		if err != nil {
			return BackupInspection{}, err
		}
		return BackupInspection{}, errors.New("sqlite: backup integrity check failed")
	}
	migrations, err := normalizeMigrations(nil)
	if err != nil {
		return BackupInspection{}, err
	}
	expected := make(map[int]string, len(migrations))
	for _, migration := range migrations {
		expected[migration.Version] = migration.Checksum
	}
	db, err := sql.Open("sqlite", sqliteURI(path, true, true))
	if err != nil {
		return BackupInspection{}, err
	}
	defer db.Close()
	rows, err := db.QueryContext(ctx, "SELECT version, checksum FROM schema_migrations ORDER BY version")
	if err != nil {
		return BackupInspection{}, errors.New("sqlite: backup schema metadata unavailable")
	}
	defer rows.Close()
	versions := make([]int, 0, len(migrations))
	for rows.Next() {
		var version int
		var appliedChecksum string
		if err := rows.Scan(&version, &appliedChecksum); err != nil {
			return BackupInspection{}, err
		}
		want, ok := expected[version]
		if !ok || appliedChecksum != want {
			return BackupInspection{}, fmt.Errorf("sqlite: backup migration checksum mismatch")
		}
		versions = append(versions, version)
	}
	if err := rows.Err(); err != nil {
		return BackupInspection{}, err
	}
	if len(versions) == 0 {
		return BackupInspection{}, errors.New("sqlite: backup has no applied schema")
	}
	sort.Ints(versions)
	for index, version := range versions {
		if version != index+1 {
			return BackupInspection{}, errors.New("sqlite: backup migration sequence is incomplete")
		}
	}
	return BackupInspection{Checksum: checksum, SchemaVersion: versions[len(versions)-1], Integrity: true, Compatible: true}, nil
}
