package config_test

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jeremy-merchant/oh-my-group/internal/config"
)

func TestLoadDefaultsMatchMaster(t *testing.T) {
	project, err := config.Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := project.Resolve(config.Inputs{})
	if err != nil {
		t.Fatal(err)
	}

	if resolved.Project.Name != "" || resolved.Project.Display != "" || resolved.Project.TaskPrefix != "OMG" || resolved.Project.DefaultBranch != "main" {
		t.Fatalf("project defaults = %+v", resolved.Project)
	}
	if resolved.Coordination.MaxDelegationDepth != 8 || resolved.Coordination.TaskClaimMode != config.TaskClaimModeExclusive || resolved.Coordination.DefaultDependencyUnblockOn != config.UnblockWhenWorkComplete || resolved.Coordination.SemanticStaleAfter != 20*time.Minute || resolved.Coordination.ProcessStaleAfter != 5*time.Minute || resolved.Coordination.AgentDeadAfter != 2*time.Hour {
		t.Fatalf("coordination defaults = %+v", resolved.Coordination)
	}
	if resolved.Reservation.DefaultTTL != 30*time.Minute || resolved.Reservation.Mode != config.ReservationModeAdvisory || !resolved.Reservation.ReleaseOnHandoff || !resolved.Reservation.RenewOnCheckpoint {
		t.Fatalf("reservation defaults = %+v", resolved.Reservation)
	}
	if resolved.Privacy.PromptStorage != config.StorageModeRedacted || resolved.Privacy.FinalOutputStorage != config.StorageModeRedacted || resolved.Privacy.BoardShowPrompt != config.BoardShowPromptSummary || resolved.Privacy.ExportSensitiveDefault {
		t.Fatalf("privacy defaults = %+v", resolved.Privacy)
	}
	if resolved.Git.ScanInterval != 5*time.Minute || !resolved.Git.IncludeUntrackedCount || !resolved.Git.DetectUnpushed || resolved.Git.CleanupMode != config.CleanupModePlanOnly {
		t.Fatalf("git defaults = %+v", resolved.Git)
	}
	if resolved.Board.DefaultFormat != config.BoardFormatTTY || !resolved.Board.HTMLSelfContained || resolved.Integrations.ManagedMarker != "omg" || resolved.Integrations.InstructionTargets != nil {
		t.Fatalf("board/integration defaults = %+v %+v", resolved.Board, resolved.Integrations)
	}
}

func TestLoadDecodesEveryMasterKey(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, ".omg", "project.toml"), `workspace = "tracked"
[project]
name = "coordination"
display = "Coordination Ledger"
task_prefix = "TASK"
default_branch = "trunk"
[coordination]
max_delegation_depth = 5
task_claim_mode = "advisory"
default_dependency_unblock_on = "verified_done"
semantic_stale_after = "16m"
process_stale_after = "6m"
agent_dead_after = "3h"
[reservation]
default_ttl = "31m"
mode = "exclusive"
release_on_handoff = false
renew_on_checkpoint = false
[privacy]
prompt_storage = "full"
final_output_storage = "full"
board_show_prompt = "full"
export_sensitive_default = true
[git]
scan_interval = "2m"
include_untracked_count = false
detect_unpushed = false
cleanup_mode = "automatic"
[board]
default_format = "html"
html_self_contained = false
[integrations]
managed_marker = "omg-test"
instruction_targets = ["runtime/INSTRUCTIONS.md"]
`)
	project, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := project.Resolve(config.Inputs{})
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Workspace != "tracked" || resolved.Project.Name != "coordination" || resolved.Project.Display != "Coordination Ledger" || resolved.Project.TaskPrefix != "TASK" || resolved.Project.DefaultBranch != "trunk" {
		t.Fatalf("project = %+v, workspace = %q", resolved.Project, resolved.Workspace)
	}
	if resolved.Coordination.MaxDelegationDepth != 5 || resolved.Coordination.TaskClaimMode != config.TaskClaimModeAdvisory || resolved.Coordination.DefaultDependencyUnblockOn != config.UnblockWhenVerifiedDone || resolved.Coordination.SemanticStaleAfter != 16*time.Minute || resolved.Coordination.ProcessStaleAfter != 6*time.Minute || resolved.Coordination.AgentDeadAfter != 3*time.Hour {
		t.Fatalf("coordination = %+v", resolved.Coordination)
	}
	if resolved.Reservation.DefaultTTL != 31*time.Minute || resolved.Reservation.Mode != config.ReservationModeExclusive || resolved.Reservation.ReleaseOnHandoff || resolved.Reservation.RenewOnCheckpoint {
		t.Fatalf("reservation = %+v", resolved.Reservation)
	}
	if resolved.Privacy.PromptStorage != config.StorageModeFull || resolved.Privacy.FinalOutputStorage != config.StorageModeFull || resolved.Privacy.BoardShowPrompt != config.BoardShowPromptFull || !resolved.Privacy.ExportSensitiveDefault {
		t.Fatalf("privacy = %+v", resolved.Privacy)
	}
	if resolved.Git.ScanInterval != 2*time.Minute || resolved.Git.IncludeUntrackedCount || resolved.Git.DetectUnpushed || resolved.Git.CleanupMode != config.CleanupModeAutomatic || resolved.Board.DefaultFormat != config.BoardFormatHTML || resolved.Board.HTMLSelfContained || resolved.Integrations.ManagedMarker != "omg-test" || len(resolved.Integrations.InstructionTargets) != 1 || resolved.Integrations.InstructionTargets[0] != "runtime/INSTRUCTIONS.md" {
		t.Fatalf("other sections = %+v %+v %+v %+v", resolved.Git, resolved.Board, resolved.Integrations, resolved.Privacy)
	}
}

func TestResolvePrecedence(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, ".omg", "project.toml"), "workspace = \"tracked\"\n[project]\ntask_prefix = \"TRACKED\"\n")
	mustWrite(t, filepath.Join(root, ".omg", "local.toml"), "workspace = \"local\"\n[project]\ntask_prefix = \"LOCAL\"\n")
	project, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := project.Resolve(config.Inputs{Environment: map[string]string{"OMG_WORKSPACE": "environment", "OMG_STORE_PATH": "environment-store", "OMG_PROJECT_TASK_PREFIX": "ENV"}})
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Workspace != "environment" || resolved.StorePath != "environment-store" || resolved.Project.TaskPrefix != "ENV" {
		t.Fatalf("environment = %+v", resolved)
	}
	resolved, err = project.Resolve(config.Inputs{Workspace: "cli", StorePath: "cli-store", Environment: map[string]string{"OMG_WORKSPACE": "environment", "OMG_STORE_PATH": "environment-store"}, Override: &config.Overrides{Project: &config.ProjectOverrides{TaskPrefix: new("CLI")}}})
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Workspace != "cli" || resolved.StorePath != "cli-store" || resolved.Project.TaskPrefix != "CLI" {
		t.Fatalf("explicit = %+v", resolved)
	}
	resolved, err = project.Resolve(config.Inputs{})
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Workspace != "local" || resolved.Project.TaskPrefix != "LOCAL" {
		t.Fatalf("local = %+v", resolved)
	}
}

func TestResolveRejectsUnknownOMGEnvironment(t *testing.T) {
	project, err := config.Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := project.Resolve(config.Inputs{Environment: map[string]string{"OMG_UNKNOWN": "value"}}); err == nil {
		t.Fatal("Resolve accepted unknown OMG environment variable")
	}
}

func TestRejectsDelegationTokensInProjectConfigurationValues(t *testing.T) {
	token := "omgdt_v1_" + strings.Repeat("a", 43)
	for _, test := range []struct {
		name  string
		value string
		load  func(t *testing.T, root, value string) error
	}{
		{
			name:  "tracked project display exact token",
			value: token,
			load: func(t *testing.T, root, value string) error {
				t.Helper()
				mustWrite(t, filepath.Join(root, ".omg", "project.toml"), "[project]\ndisplay = "+strconv.Quote(value)+"\n")
				_, err := config.Load(root)
				return err
			},
		},
		{
			name:  "tracked project display suffixed token",
			value: token + "recoverable",
			load: func(t *testing.T, root, value string) error {
				t.Helper()
				mustWrite(t, filepath.Join(root, ".omg", "project.toml"), "[project]\ndisplay = "+strconv.Quote(value)+"\n")
				_, err := config.Load(root)
				return err
			},
		},
		{
			name:  "local project display exact token",
			value: token,
			load: func(t *testing.T, root, value string) error {
				t.Helper()
				mustWrite(t, filepath.Join(root, ".omg", "local.toml"), "[project]\ndisplay = "+strconv.Quote(value)+"\n")
				_, err := config.Load(root)
				return err
			},
		},
		{
			name:  "local project display suffixed token",
			value: token + "recoverable",
			load: func(t *testing.T, root, value string) error {
				t.Helper()
				mustWrite(t, filepath.Join(root, ".omg", "local.toml"), "[project]\ndisplay = "+strconv.Quote(value)+"\n")
				_, err := config.Load(root)
				return err
			},
		},
		{
			name:  "environment exact token",
			value: token,
			load: func(t *testing.T, root, value string) error {
				t.Helper()
				project, err := config.Load(root)
				if err != nil {
					return err
				}
				_, err = project.Resolve(config.Inputs{Environment: map[string]string{"OMG_PROJECT_DISPLAY": value}})
				return err
			},
		},
		{
			name:  "environment suffixed token",
			value: token + "recoverable",
			load: func(t *testing.T, root, value string) error {
				t.Helper()
				project, err := config.Load(root)
				if err != nil {
					return err
				}
				_, err = project.Resolve(config.Inputs{Environment: map[string]string{"OMG_PROJECT_DISPLAY": value}})
				return err
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := test.load(t, t.TempDir(), test.value); err == nil {
				t.Fatal("delegation token accepted")
			}
		})
	}
}

func TestPermitsIncompleteDelegationTokenLookalikesInProjectConfiguration(t *testing.T) {
	lookalike := "omgdt_v1_" + strings.Repeat("a", 42)
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, ".omg", "project.toml"), "[project]\ndisplay = "+strconv.Quote(lookalike)+"\n")
	project, err := config.Load(root)
	if err != nil {
		t.Fatalf("Load rejected ordinary token-like display: %v", err)
	}
	resolved, err := project.Resolve(config.Inputs{Environment: map[string]string{"OMG_PROJECT_DISPLAY": lookalike}})
	if err != nil {
		t.Fatalf("Resolve rejected ordinary token-like environment value: %v", err)
	}
	if resolved.Project.Display != lookalike {
		t.Fatalf("display = %q, want %q", resolved.Project.Display, lookalike)
	}
}

func TestLoadRejectsInvalidAndCrossFieldConfig(t *testing.T) {
	for name, document := range map[string]string{
		"unknown":               "unknown = \"no\"\n",
		"legacy spelling":       "[coordination]\nexclusive_claim_mode = \"required\"\n",
		"invalid enum":          "[privacy]\nprompt_storage = \"plaintext\"\n",
		"invalid duration":      "[coordination]\nsemantic_stale_after = \"soon\"\n",
		"non-positive duration": "[reservation]\ndefault_ttl = \"0s\"\n",
		"max depth":             "[coordination]\nmax_delegation_depth = 0\n",
		"staleness ordering":    "[coordination]\nsemantic_stale_after = \"4m\"\nprocess_stale_after = \"5m\"\nagent_dead_after = \"2h\"\n",
		"invalid task prefix":   "[project]\ntask_prefix = \"bad prefix\"\n",
		"invalid marker":        "[integrations]\nmanaged_marker = \"not valid!\"\n",
		"invalid project name":  "[project]\nname = \"Bad Name\"\n",
	} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			mustWrite(t, filepath.Join(root, ".omg", "project.toml"), document)
			project, err := config.Load(root)
			if err != nil {
				return
			}
			if _, err := project.Resolve(config.Inputs{}); err == nil {
				t.Fatal("configuration succeeded")
			}
		})
	}
}

func TestResolveValidatesExplicitCrossFieldValues(t *testing.T) {
	project, err := config.Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := project.Resolve(config.Inputs{Override: &config.Overrides{Coordination: &config.CoordinationOverrides{
		SemanticStaleAfter: new("3m"),
		ProcessStaleAfter:  new("5m"),
		AgentDeadAfter:     new("2h"),
	}}}); err == nil {
		t.Fatal("Resolve accepted invalid staleness ordering")
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
