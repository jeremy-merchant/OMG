package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func projectConfinedAgentHome(t *testing.T) string {
	t.Helper()
	working, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	directory := filepath.Join(working, ".agent-install-fixtures", strings.ReplaceAll(t.Name(), "/", "-"))
	if err := os.RemoveAll(directory); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(filepath.Join(working, ".agent-install-fixtures")) })
	return directory
}

func TestAgentInstallLifecycleAndTTY(t *testing.T) {
	home := projectConfinedAgentHome(t)
	t.Setenv("OMG_AGENT_HOME", home)
	t.Setenv("NO_COLOR", "1")
	t.Setenv("COLUMNS", "88")

	var output bytes.Buffer
	exit := Run([]string{"agent", "install"}, "test-version", &output)
	if exit != ExitSuccess {
		t.Fatalf("agent install exit=%d output=%q", exit, output.String())
	}
	for _, want := range []string{"OMG  AGENT HARNESS", "Discovery surfaces", "~/.agents/skills/omg/SKILL.md", "Claude", "Codex", "OMP"} {
		if !strings.Contains(output.String(), want) {
			t.Errorf("agent install TTY missing %q:\n%s", want, output.String())
		}
	}
	if strings.Contains(output.String(), home) {
		t.Fatalf("agent install exposed absolute home: %s", output.String())
	}

	output.Reset()
	exit = Run([]string{"agent", "doctor", "--json"}, "test-version", &output)
	if exit != ExitSuccess {
		t.Fatalf("agent doctor exit=%d output=%q", exit, output.String())
	}
	var envelope struct {
		Data struct {
			Status   string `json:"status"`
			Home     string `json:"home"`
			Surfaces []struct {
				Path  string `json:"path"`
				State string `json:"state"`
			} `json:"surfaces"`
			Summary struct {
				Installed int `json:"installed"`
				Missing   int `json:"missing"`
				Drifted   int `json:"drifted"`
				Unsafe    int `json:"unsafe"`
			} `json:"summary"`
		} `json:"data"`
	}
	if err := json.Unmarshal(output.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	report := envelope.Data
	if report.Status != "healthy" || report.Home != "~" || report.Summary.Installed != len(report.Surfaces) || report.Summary.Missing != 0 || report.Summary.Drifted != 0 || report.Summary.Unsafe != 0 {
		t.Fatalf("unexpected agent doctor report: %+v", report)
	}
	for _, surface := range report.Surfaces {
		if !strings.HasPrefix(surface.Path, "~/") || surface.State != "installed" {
			t.Fatalf("unsafe agent surface projection: %+v", surface)
		}
	}

	output.Reset()
	exit = Run([]string{"agent", "install", "--json"}, "test-version", &output)
	if exit != ExitSuccess {
		t.Fatalf("agent install replay exit=%d output=%q", exit, output.String())
	}

	output.Reset()
	exit = Run([]string{"agent", "uninstall", "--json"}, "test-version", &output)
	if exit != ExitSuccess {
		t.Fatalf("agent uninstall exit=%d output=%q", exit, output.String())
	}
	if err := json.Unmarshal(output.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	report = envelope.Data
	if report.Status != "uninstalled" || report.Summary.Missing != len(report.Surfaces) {
		t.Fatalf("unexpected agent uninstall report: %+v", report)
	}
	if _, err := os.Stat(filepath.Join(home, ".agents", "skills", "omg", "SKILL.md")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("agent uninstall left managed skill: %v", err)
	}
}

func TestAgentCommandRejectsProjectSelection(t *testing.T) {
	home := projectConfinedAgentHome(t)
	t.Setenv("OMG_AGENT_HOME", home)
	var output bytes.Buffer
	exit := Run([]string{"agent", "status", "--project", home, "--json"}, "test-version", &output)
	if exit != ExitUsage || !strings.Contains(output.String(), "agent request is invalid") {
		t.Fatalf("agent accepted project selection exit=%d output=%q", exit, output.String())
	}
}
