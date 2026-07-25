package instructions

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestApplyPreflightsEveryTargetBeforeMutation(t *testing.T) {
	root := t.TempDir()
	first := filepath.Join(root, "first.md")
	second := filepath.Join(root, "second.md")
	if err := os.WriteFile(first, []byte("first"), 0644); err != nil {
		t.Fatal(err)
	}
	malformed := []byte("<!-- OMG BEGIN v1 -->\n")
	if err := os.WriteFile(second, malformed, 0644); err != nil {
		t.Fatal(err)
	}
	s, err := New(root, []Target{{Path: "first.md"}, {Path: "second.md"}}, "x")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Apply(); !errors.Is(err, ErrMalformedBlock) {
		t.Fatalf("Apply error = %v", err)
	}
	got, err := os.ReadFile(first)
	if err != nil || string(got) != "first" {
		t.Fatalf("earlier target changed: %q, %v", got, err)
	}
}

func TestReplaceRejectsConcurrentChanges(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(t *testing.T, root, path string)
	}{
		{"content", func(t *testing.T, _ string, path string) {
			t.Helper()
			if err := os.WriteFile(path, []byte("racer"), 0644); err != nil {
				t.Fatal(err)
			}
		}},
		{"created", func(t *testing.T, _ string, path string) {
			t.Helper()
			if err := os.WriteFile(path, []byte("racer"), 0644); err != nil {
				t.Fatal(err)
			}
		}},
		{"symlink", func(t *testing.T, root, path string) {
			t.Helper()
			real := filepath.Join(root, "real")
			if err := os.WriteFile(real, []byte("real"), 0644); err != nil {
				t.Fatal(err)
			}
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(real, path); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, "target.md")
			if tc.name != "created" {
				if err := os.WriteFile(path, []byte("original"), 0644); err != nil {
					t.Fatal(err)
				}
			}
			s, err := New(root, []Target{{Path: "target.md"}}, "managed")
			if err != nil {
				t.Fatal(err)
			}
			s.beforeRename = func() error { tc.change(t, root, path); return nil }
			if _, err := s.Apply(); !errors.Is(err, ErrChanged) {
				t.Fatalf("Apply error = %v", err)
			}
			if tc.name == "content" {
				got, _ := os.ReadFile(path)
				if !bytes.Equal(got, []byte("racer")) {
					t.Fatalf("content overwritten: %q", got)
				}
			}
			if tc.name == "symlink" {
				info, err := os.Lstat(path)
				if err != nil || info.Mode()&os.ModeSymlink == 0 {
					t.Fatalf("symlink overwritten: %v", err)
				}
			}
		})
	}
}

func TestStatusAndDiffReportManagedDrift(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "guide.md")
	if err := os.WriteFile(path, []byte("base"), 0644); err != nil {
		t.Fatal(err)
	}
	s, err := New(root, []Target{{Path: "guide.md"}}, "one")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Apply(); err != nil {
		t.Fatal(err)
	}
	s.instructions = "two"
	status, err := s.Status()
	if err != nil {
		t.Fatal(err)
	}
	if status[0].Action != ActionUpdate || !status[0].Managed {
		t.Fatalf("status = %#v", status[0])
	}
	plan, err := s.Plan()
	if err != nil {
		t.Fatal(err)
	}
	if plan[0].Action != ActionUpdate ||
		!strings.HasPrefix(plan[0].Diff, "--- a/guide.md\n+++ b/guide.md\n@@ -1,5 +1,5 @@\n") ||
		!strings.Contains(plan[0].Diff, "-one\n") ||
		!strings.Contains(plan[0].Diff, "+two\n") {
		t.Fatalf("plan = %#v", plan[0])
	}
	remove, err := s.PlanRemoval()
	if err != nil {
		t.Fatal(err)
	}
	if remove[0].Action != ActionRemove || remove[0].Diff == "" {
		t.Fatalf("remove plan = %#v", remove[0])
	}
}
