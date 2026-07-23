//go:build windows

package watch

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

func privateWindowsStateDir(t *testing.T) string {
	return newPrivateStateDir(t)
}

func newPrivateStateDir(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "state")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	sid, err := currentUserSID()
	if err != nil {
		t.Fatal(err)
	}
	if err := applyPrivateDACL(dir, sid); err != nil {
		t.Fatal(err)
	}
	if err := validatePrivateDACL(dir, sid); err != nil {
		t.Fatal(err)
	}
	return dir
}

func newBroadStateDir(t *testing.T, root string) string {
	t.Helper()
	dir := filepath.Join(root, "unsafe")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := applyBroadWatchDACL(dir); err != nil {
		t.Fatal(err)
	}
	return dir
}

func writePrivateTestFile(path string, data []byte) error {
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return err
	}
	return secureNewPrivateFile(path)
}

func applyBroadWatchDACL(path string) error {
	descriptor, err := windows.SecurityDescriptorFromString("D:P(A;;FA;;;WD)")
	if err != nil {
		return err
	}
	dacl, _, err := descriptor.DACL()
	if err != nil {
		return err
	}
	return windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, dacl, nil)
}

func TestWindowsWatchInitStartStatusAndStaleRecovery(t *testing.T) {
	clock := &testClock{now: time.Date(2026, 7, 23, 12, 2, 0, 0, time.UTC)}
	engine, err := New(testConfig(t, privateWindowsStateDir(t), clock, newTestTicker()))
	if err != nil {
		t.Fatal(err)
	}
	if got := engine.Status(context.Background()).Code; got != StatusStopped {
		t.Fatalf("initial status = %q, want %q", got, StatusStopped)
	}
	if !engine.createLock() {
		t.Fatal("creating protected main lock failed")
	}
	if err := engine.writeLease(clock.now); err != nil {
		t.Fatal(err)
	}
	if got := engine.Status(context.Background()).Code; got != StatusActive {
		t.Fatalf("started status = %q, want %q", got, StatusActive)
	}
	if err := engine.writeLease(clock.now.Add(-2 * time.Minute)); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if got := engine.Run(ctx).Code; got != ResultStopped {
		t.Fatalf("stale recovery result = %q, want %q", got, ResultStopped)
	}
	sid, err := currentUserSID()
	if err != nil {
		t.Fatal(err)
	}
	if err := validatePrivateDACL(engine.recoveryLockPath(), sid); err != nil {
		t.Fatalf("recovery lock DACL: %v", err)
	}
}

func TestWindowsWatchRejectsReparseAndBroadPaths(t *testing.T) {
	clock := &testClock{now: time.Now().UTC()}
	root := t.TempDir()
	target := privateWindowsStateDir(t)
	reparse := filepath.Join(root, "reparse")
	if err := os.Symlink(target, reparse); err != nil {
		t.Skipf("cannot create directory reparse point: %v", err)
	}
	if _, err := New(testConfig(t, reparse, clock, newTestTicker())); err == nil {
		t.Fatal("New accepted reparse state directory")
	}

	broad := filepath.Join(root, "broad")
	if err := os.Mkdir(broad, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := applyBroadWatchDACL(broad); err != nil {
		t.Fatal(err)
	}
	if _, err := New(testConfig(t, broad, clock, newTestTicker())); err == nil {
		t.Fatal("New accepted broadly accessible state directory")
	}
}

func TestWindowsWatchRejectsReparseLeaseAndLocks(t *testing.T) {
	clock := &testClock{now: time.Now().UTC()}
	engine, err := New(testConfig(t, privateWindowsStateDir(t), clock, newTestTicker()))
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{engine.leasePath(), engine.lockPath(), engine.recoveryLockPath()} {
		target := path + ".target"
		if err := os.WriteFile(target, []byte(engine.nonce), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := secureNewPrivateFile(target); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, path); err != nil {
			t.Skipf("cannot create file reparse point: %v", err)
		}
		info, err := os.Lstat(path)
		if err != nil {
			t.Fatal(err)
		}
		if validatePrivateRegularFile(path, info) {
			t.Fatalf("accepted reparse watch artifact %q", path)
		}
		if path == engine.leasePath() && engine.Status(context.Background()).Code != StatusUnknown {
			t.Fatal("reparse lease did not fail closed")
		}
		if path == engine.lockPath() && engine.reclaimable(context.Background()) {
			t.Fatal("reparse main lock was reclaimable")
		}
		if path == engine.recoveryLockPath() {
			if release, ok := acquireRecoveryGuard(path); ok {
				release()
				t.Fatal("reparse recovery lock was accepted")
			}
		}
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
	}
}

func TestWindowsWatchRejectsBroadLeaseMainAndRecoveryLocks(t *testing.T) {
	clock := &testClock{now: time.Now().UTC()}
	engine, err := New(testConfig(t, privateWindowsStateDir(t), clock, newTestTicker()))
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{engine.leasePath(), engine.lockPath(), engine.recoveryLockPath()} {
		if err := os.WriteFile(path, []byte(engine.nonce), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := applyBroadWatchDACL(path); err != nil {
			t.Fatal(err)
		}
		info, err := os.Lstat(path)
		if err != nil {
			t.Fatal(err)
		}
		if validatePrivateRegularFile(path, info) {
			t.Fatalf("accepted broad watch artifact %q", path)
		}
		if path == engine.leasePath() {
			if got := engine.Status(context.Background()).Code; got != StatusUnknown {
				t.Fatalf("broad lease status = %q, want %q", got, StatusUnknown)
			}
		}
		_ = os.Remove(path)
	}
}
