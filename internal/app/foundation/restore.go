package foundation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"

	"github.com/jeremy-merchant/OMG/internal/domain"
	"github.com/jeremy-merchant/OMG/internal/ports"
)

type RestorePlanRequest struct {
	BackupPath      string `json:"backup_path"`
	BackupChecksum  string `json:"backup_checksum"`
	DestinationPath string `json:"destination_path"`
}

type RestorePlan struct {
	PlanID                 string `json:"plan_id"`
	Project                string `json:"project"`
	BackupChecksum         string `json:"backup_checksum"`
	SchemaVersion          int    `json:"schema_version"`
	DestinationFingerprint string `json:"destination_fingerprint"`
	Integrity              bool   `json:"integrity"`
	Compatible             bool   `json:"compatible"`
	ApplyAvailable         bool   `json:"apply_available"`
	RequiredAction         string `json:"required_action"`
}

// PlanRestore validates a candidate backup and a fresh destination without
// creating, copying, replacing, or opening the destination. v0.1 deliberately
// leaves restore application to a separately approved external procedure.
func (s *Service) PlanRestore(ctx context.Context, selection Selection, request RestorePlanRequest) (RestorePlan, domain.DomainError) {
	if s == nil || s.resolver == nil || s.inspectBackup == nil || s.pathInspector == nil || !filepath.IsAbs(request.BackupPath) || !filepath.IsAbs(request.DestinationPath) || request.BackupChecksum == "" {
		return RestorePlan{}, domain.NewError(domain.CodeInvalidArgument, "restore plan request is invalid", false)
	}
	backupPath := filepath.Clean(request.BackupPath)
	destinationPath := filepath.Clean(request.DestinationPath)
	if backupPath == destinationPath || !s.pathInspector.FreshDestination(destinationPath) {
		return RestorePlan{}, domain.NewError(domain.CodeInvalidArgument, "restore destination is not a fresh safe path", false)
	}
	resolved, resolveErr := s.resolver.Resolve(ctx, ports.ResolveRequest{ProjectPath: selection.Project, WorkspacePath: selection.Workspace, StorePath: selection.Store})
	if resolveErr != nil || resolved.Project == "" {
		return RestorePlan{}, unavailable()
	}
	inspection, inspectErr := s.inspectBackup(ctx, backupPath, request.BackupChecksum)
	if inspectErr != nil || !inspection.Integrity || !inspection.Compatible {
		return RestorePlan{}, domain.NewError(domain.CodeConflict, "backup is not compatible and integrity-validated", false)
	}
	destinationFingerprint := digestText(destinationPath)
	planID := digestText(string(resolved.Project) + "\x00" + inspection.Checksum + "\x00" + destinationFingerprint)
	return RestorePlan{
		PlanID:                 planID,
		Project:                string(resolved.Project),
		BackupChecksum:         inspection.Checksum,
		SchemaVersion:          inspection.SchemaVersion,
		DestinationFingerprint: destinationFingerprint,
		Integrity:              true,
		Compatible:             true,
		ApplyAvailable:         false,
		RequiredAction:         "obtain separate human approval and follow the external fresh-destination restore procedure",
	}, domain.DomainError{}
}

func digestText(value string) string {
	digest := sha256.Sum256([]byte(value))
	return "sha256:" + hex.EncodeToString(digest[:])
}
