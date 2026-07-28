package cli

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

type failingPlanOutputFile struct {
	*os.File
	stage  string
	failed *bool
}

func (file failingPlanOutputFile) Chmod(mode os.FileMode) error {
	if file.stage == "chmod" {
		*file.failed = true
		return errors.New("injected chmod failure")
	}
	return file.File.Chmod(mode)
}

func (file failingPlanOutputFile) Write(data []byte) (int, error) {
	if file.stage == "write" {
		*file.failed = true
		return 0, errors.New("injected write failure")
	}
	return file.File.Write(data)
}

func (file failingPlanOutputFile) Sync() error {
	if file.stage == "sync" {
		*file.failed = true
		return errors.New("injected sync failure")
	}
	return file.File.Sync()
}

func (file failingPlanOutputFile) Close() error {
	err := file.File.Close()
	if file.stage == "close" {
		*file.failed = true
		return errors.New("injected close failure")
	}
	return err
}

func privatePlanOutputPath(t *testing.T) string {
	t.Helper()

	parent, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(parent, "plan.json")
}

func TestWriteNewPrivatePlanRemovesOnlyIncompleteCreatedOutput(t *testing.T) {
	originalCreate := createNewPrivatePlanOutputFile
	t.Cleanup(func() { createNewPrivatePlanOutputFile = originalCreate })

	for _, stage := range []string{"chmod", "write", "sync", "close"} {
		t.Run(stage, func(t *testing.T) {
			t.Cleanup(func() { createNewPrivatePlanOutputFile = originalCreate })
			path := privatePlanOutputPath(t)
			failed := false
			createNewPrivatePlanOutputFile = func(path string) (privatePlanOutputFile, error) {
				created, err := originalCreate(path)
				if err != nil {
					return nil, err
				}
				file, ok := created.(*os.File)
				if !ok {
					return nil, errors.New("unexpected plan output file type")
				}
				return failingPlanOutputFile{File: file, stage: stage, failed: &failed}, nil
			}

			if err := writeNewPrivatePlan(path, []byte("partial")); err == nil {
				t.Fatal("writeNewPrivatePlan succeeded despite injected failure")
			}
			if !failed {
				t.Fatalf("%s failure was not injected", stage)
			}
			if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("incomplete plan remained after %s failure: %v", stage, err)
			}
		})
	}
}

func TestWriteNewPrivatePlanPreservesExistingDestination(t *testing.T) {
	path := privatePlanOutputPath(t)
	original := []byte("existing plan")
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}

	if err := writeNewPrivatePlan(path, []byte("replacement")); err == nil {
		t.Fatal("writeNewPrivatePlan overwrote an existing destination")
	}
	actual, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(actual) != string(original) {
		t.Fatalf("existing destination changed: got %q want %q", actual, original)
	}
}

func TestRemovePrivatePlanOutputIfSameFilePreservesReplacement(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows prevents replacing an open plan output file")
	}
	path := filepath.Join(t.TempDir(), "plan.json")
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	created, err := file.Stat()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = file.Close() })
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	replacement := []byte("replacement plan")
	if err := os.WriteFile(path, replacement, 0o600); err != nil {
		t.Fatal(err)
	}

	removePrivatePlanOutputIfSameFile(path, created)

	actual, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(actual) != string(replacement) {
		t.Fatalf("replacement destination changed: got %q want %q", actual, replacement)
	}
}

func TestWriteNewPrivatePlanCreatesCompletePrivateOutput(t *testing.T) {
	path := privatePlanOutputPath(t)
	data := []byte("complete plan")
	if err := writeNewPrivatePlan(path, data); err != nil {
		t.Fatal(err)
	}

	actual, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(actual) != string(data) {
		t.Fatalf("plan contents = %q, want %q", actual, data)
	}
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
		t.Fatalf("plan mode = %v, want regular 0600", info.Mode())
	}
}
