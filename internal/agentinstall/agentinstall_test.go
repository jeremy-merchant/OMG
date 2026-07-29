package agentinstall

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallStatusAndUninstall(t *testing.T) {
	home := t.TempDir()
	original := "# Existing Claude rules\n"
	claudeDir := filepath.Join(home, ".claude")
	if err := os.MkdirAll(claudeDir, 0o700); err != nil {
		t.Fatal(err)
	}
	claudePath := filepath.Join(claudeDir, "CLAUDE.md")
	if err := os.WriteFile(claudePath, []byte(original), 0o640); err != nil {
		t.Fatal(err)
	}

	service, err := New(home)
	if err != nil {
		t.Fatal(err)
	}
	service.lookPath = func(name string) (string, error) {
		if name == "claude" || name == "omp" {
			return "/detected/" + name, nil
		}
		return "", errors.New("missing")
	}

	before, err := service.Status()
	if err != nil {
		t.Fatal(err)
	}
	if before.Summary.Missing != len(instructionSpecs)+len(skillSpecs) {
		t.Fatalf("unexpected pre-install summary: %+v", before.Summary)
	}

	installed, err := service.Install()
	if err != nil {
		t.Fatal(err)
	}
	if installed.Status != "installed" || installed.Summary.Installed != len(instructionSpecs)+len(skillSpecs) {
		t.Fatalf("unexpected install report: %+v", installed)
	}
	if installed.Summary.Detected != 2 { // Claude and OMP providers.
		t.Fatalf("unexpected detected count: %+v", installed.Summary)
	}
	data, err := os.ReadFile(claudePath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(data), original) || !strings.Contains(string(data), InstructionContent()) {
		t.Fatalf("existing Claude content was not preserved: %q", data)
	}
	mode, err := os.Stat(claudePath)
	if err != nil {
		t.Fatal(err)
	}
	if mode.Mode().Perm() != 0o640 {
		t.Fatalf("existing mode changed: %#o", mode.Mode().Perm())
	}

	replayed, err := service.Install()
	if err != nil {
		t.Fatal(err)
	}
	if replayed.Summary.Installed != installed.Summary.Installed {
		t.Fatalf("install replay drifted: %+v", replayed)
	}

	removed, err := service.Uninstall()
	if err != nil {
		t.Fatal(err)
	}
	if removed.Status != "uninstalled" || removed.Summary.Missing != len(instructionSpecs)+len(skillSpecs) {
		t.Fatalf("unexpected uninstall report: %+v", removed)
	}
	data, err = os.ReadFile(claudePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != original {
		t.Fatalf("uninstall did not restore existing content: %q", data)
	}
	if _, err := os.Stat(filepath.Join(home, ".agents", "skills", "omg", "SKILL.md")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("managed skill remains after uninstall: %v", err)
	}
}

func TestInstallRejectsForeignSkill(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, ".agents", "skills", "omg", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("foreign skill\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	service, err := New(home)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Install(); !errors.Is(err, ErrConflict) {
		t.Fatalf("foreign skill was not rejected: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "foreign skill\n" {
		t.Fatalf("foreign skill changed: %q", data)
	}
	if _, err := os.Stat(filepath.Join(home, ".claude")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("foreign-skill preflight created unrelated provider directories: %v", err)
	}
}

func TestInstallUpdatesPreviousManagedSkill(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, ".agents", "skills", "omg", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	previous := strings.Replace(string(SkillContent()), "schemaVersion: 8", "schemaVersion: 7", 1)
	if err := os.WriteFile(path, []byte(previous), 0o640); err != nil {
		t.Fatal(err)
	}
	service, err := New(home)
	if err != nil {
		t.Fatal(err)
	}
	report, err := service.Status()
	if err != nil {
		t.Fatal(err)
	}
	if report.Surfaces[len(instructionSpecs)].State != StateDrifted {
		t.Fatalf("previous managed skill was not reported as drifted: %+v", report.Surfaces)
	}
	if _, err := service.Install(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != string(SkillContent()) {
		t.Fatalf("previous managed skill was not upgraded")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o640 {
		t.Fatalf("managed skill mode changed during upgrade: %#o", info.Mode().Perm())
	}
}

func TestStatusRejectsSymlinkParent(t *testing.T) {
	home := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(home, ".claude")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	service, err := New(home)
	if err != nil {
		t.Fatal(err)
	}
	report, err := service.Status()
	if err != nil {
		t.Fatal(err)
	}
	var claudeUnsafe bool
	for _, surface := range report.Surfaces {
		if surface.Provider == "Claude" && surface.State == StateUnsafe {
			claudeUnsafe = true
		}
	}
	if !claudeUnsafe {
		t.Fatalf("symlinked provider parent was not marked unsafe: %+v", report.Surfaces)
	}
	if _, err := service.Install(); !errors.Is(err, ErrUnsafeHome) && !errors.Is(err, ErrConflict) {
		t.Fatalf("symlinked provider parent was not rejected: %v", err)
	}
}

func TestSkillContentIsAlwaysApplyAndSecretFree(t *testing.T) {
	content := string(SkillContent())
	for _, required := range []string{"name: omg", "alwaysApply: true", "schemaVersion: 8", managedSkillMarker, managedSkillEnd, "The agent performs this lifecycle itself", "OBSERVE", "WORK_LITE", "FULL", "OMG records coordination risk, not every action", "Host-level tool installation", "not OMG project work", "Never use agent-harness health as a universal shell gate", "omg agent status|doctor|install|uninstall", "must not block diagnosis", "every exact pending migration", "plan-bound backup", "do not wait for human approval", "omg worker bootstrap", "OMG_CONTROLLER_SESSION_ID", "omg board me", "omg board actionable", "omg example show session-create", "omg message inbox", "instruction_source", "Git is the single source of truth", "live, non-persisted risk and ownership overlay", "native read-only Git commands", "explicit durable audit observation", "project-scoped", "VERIFIED_DONE"} {
		if !strings.Contains(content, required) {
			t.Fatalf("skill missing %q", required)
		}
	}
	for _, forbidden := range []string{"omgdt_", "Authorization:", "PRIVATE KEY", "/Users/", "C:\\Users\\"} {
		if strings.Contains(content, forbidden) {
			t.Fatalf("skill contains forbidden value %q", forbidden)
		}
	}
}
