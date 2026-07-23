package app

import (
	"context"

	"example.invalid/coordledger/internal/app/foundation"
	"example.invalid/coordledger/internal/platform"
	"example.invalid/coordledger/internal/ports"
	"example.invalid/coordledger/internal/store/sqlite"
)

func dispatcherTestDependencies(resolver ports.StoreResolver) foundation.Dependencies {
	return foundation.Dependencies{
		Resolver:          resolver,
		ConfigInitializer: platform.NewProjectConfigInitializer(),
		Open: func(ctx context.Context, path string, options ports.OpenOptions) (ports.FoundationStore, ports.OpenStatus, error) {
			store, status, err := sqlite.Open(ctx, path, options)
			if err != nil {
				return nil, ports.OpenStatus{}, err
			}
			return store, status, nil
		},
		InspectBackup: func(ctx context.Context, path, checksum string) (ports.BackupInspection, error) {
			inspection, err := sqlite.InspectBackup(ctx, path, checksum)
			if err != nil {
				return ports.BackupInspection{}, err
			}
			return ports.BackupInspection{Checksum: inspection.Checksum, SchemaVersion: inspection.SchemaVersion, Integrity: inspection.Integrity, Compatible: inspection.Compatible}, nil
		},
	}
}
