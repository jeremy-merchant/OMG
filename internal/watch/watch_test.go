package watch

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type testClock struct{ now time.Time }

func (c *testClock) Now() time.Time { return c.now }

type testTicker struct{ ticks chan time.Time }

func newTestTicker() *testTicker              { return &testTicker{ticks: make(chan time.Time, 8)} }
func (t *testTicker) Ticks() <-chan time.Time { return t.ticks }
func (t *testTicker) Stop()                   {}

type testObserver struct{ result Observation }

func (o testObserver) Observe(context.Context, int, string) Observation { return o.result }

func testConfig(t *testing.T, dir string, clock *testClock, ticker *testTicker) Config {
	t.Helper()
	return Config{
		StateDir: dir,
		PID:      1234,
		TTL:      time.Minute,
		Clock:    clock,
		Ticker:   func(time.Duration) Ticker { return ticker },
		Observer: testObserver{result: ObservationMatch},
		Nonce:    func() (string, error) { return "0123456789abcdef0123456789abcdef", nil },
	}
}

func privateStateDir(t *testing.T) string {
	t.Helper()
	return newPrivateStateDir(t)
}

func TestStatusStoppedWithoutLease(t *testing.T) {
	clock := &testClock{now: time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)}
	engine, err := New(testConfig(t, privateStateDir(t), clock, newTestTicker()))
	if err != nil {
		t.Fatal(err)
	}
	if got := engine.Status(context.Background()).Code; got != StatusStopped {
		t.Fatalf("status = %q, want %q", got, StatusStopped)
	}
}

func TestRunReportsActiveRefreshesAndCleansOwnLease(t *testing.T) {
	clock := &testClock{now: time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)}
	ticker := newTestTicker()
	config := testConfig(t, privateStateDir(t), clock, ticker)
	called := make(chan struct{}, 1)
	config.Callbacks = []Callback{func(context.Context) error { called <- struct{}{}; return nil }}
	engine, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan Result, 1)
	go func() { done <- engine.Run(ctx) }()
	deadline := time.After(time.Second)
	for engine.Status(context.Background()).Code != StatusActive {
		select {
		case <-deadline:
			t.Fatal("watch never became active")
		default:
			time.Sleep(time.Millisecond)
		}
	}
	ticker.ticks <- clock.now.Add(time.Second)
	select {
	case <-called:
	case <-time.After(time.Second):
		t.Fatal("callback was not invoked")
	}
	cancel()
	if result := <-done; result.Code != ResultStopped {
		t.Fatalf("run result = %q, want %q", result.Code, ResultStopped)
	}
	if got := engine.Status(context.Background()).Code; got != StatusStopped {
		t.Fatalf("status after cancellation = %q, want %q", got, StatusStopped)
	}
}

func TestStatusDoesNotTreatReusedPIDAsActive(t *testing.T) {
	clock := &testClock{now: time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)}
	config := testConfig(t, privateStateDir(t), clock, newTestTicker())
	engine, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	if err := engine.writeLease(clock.now); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(engine.leasePath()) })
	engine.observer = testObserver{result: ObservationMismatch}
	if got := engine.Status(context.Background()).Code; got != StatusStale {
		t.Fatalf("reused pid status = %q, want %q", got, StatusStale)
	}
}

func TestStatusFailsClosedForCorruptOrOversizeLease(t *testing.T) {
	for _, content := range [][]byte{[]byte("not-json"), make([]byte, maxLeaseBytes+1)} {
		t.Run("invalid", func(t *testing.T) {
			clock := &testClock{now: time.Now().UTC()}
			engine, err := New(testConfig(t, privateStateDir(t), clock, newTestTicker()))
			if err != nil {
				t.Fatal(err)
			}
			if err := writePrivateTestFile(engine.leasePath(), content); err != nil {
				t.Fatal(err)
			}
			if got := engine.Status(context.Background()).Code; got != StatusUnknown {
				t.Fatalf("status = %q, want %q", got, StatusUnknown)
			}
		})
	}
}

func TestConcurrentStartConflictsWithoutOverwritingOwner(t *testing.T) {
	clock := &testClock{now: time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)}
	dir := privateStateDir(t)
	first, err := New(testConfig(t, dir, clock, newTestTicker()))
	if err != nil {
		t.Fatal(err)
	}
	secondConfig := testConfig(t, dir, clock, newTestTicker())
	secondConfig.Nonce = func() (string, error) { return "fedcba9876543210fedcba9876543210", nil }
	second, err := New(secondConfig)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan Result, 1)
	go func() { done <- first.Run(ctx) }()
	deadline := time.After(time.Second)
	for first.Status(context.Background()).Code != StatusActive {
		select {
		case <-deadline:
			t.Fatal("first did not start")
		default:
			time.Sleep(time.Millisecond)
		}
	}
	if got := second.Run(context.Background()).Code; got != ResultConflict {
		t.Fatalf("second result = %q, want %q", got, ResultConflict)
	}
	if got := first.Status(context.Background()).Code; got != StatusActive {
		t.Fatalf("first status after conflict = %q, want %q", got, StatusActive)
	}
	cancel()
	<-done
}

func TestCallbackErrorIsSafeAndDoesNotCorruptLease(t *testing.T) {
	clock := &testClock{now: time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)}
	ticker := newTestTicker()
	config := testConfig(t, privateStateDir(t), clock, ticker)
	called := make(chan struct{}, 1)
	config.Callbacks = []Callback{func(context.Context) error {
		called <- struct{}{}
		return errors.New("private path /secret")
	}}
	engine, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan Result, 1)
	go func() { done <- engine.Run(ctx) }()
	deadline := time.After(time.Second)
	for engine.Status(context.Background()).Code != StatusActive {
		select {
		case <-deadline:
			t.Fatal("watch never became active")
		default:
			time.Sleep(time.Millisecond)
		}
	}
	ticker.ticks <- clock.now.Add(time.Second)
	select {
	case <-called:
	case <-time.After(time.Second):
		t.Fatal("callback was not invoked")
	}
	if got := engine.Status(context.Background()).Code; got != StatusActive {
		t.Fatalf("status = %q, want active", got)
	}
	cancel()
	result := <-done
	if len(result.Callbacks) != 1 || result.Callbacks[0] != CallbackFailed {
		t.Fatalf("callback results = %#v, want safe failure", result.Callbacks)
	}
}

func TestCancellationDoesNotRemoveAnotherOwnersLease(t *testing.T) {
	clock := &testClock{now: time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)}
	engine, err := New(testConfig(t, privateStateDir(t), clock, newTestTicker()))
	if err != nil {
		t.Fatal(err)
	}
	if err := engine.writeLease(clock.now); err != nil {
		t.Fatal(err)
	}
	if err := writePrivateTestFile(engine.leasePath(), []byte(`{"version":1,"pid":1234,"nonce":"fedcba9876543210fedcba9876543210","heartbeat":"2026-07-23T12:00:00Z"}`)); err != nil {
		t.Fatal(err)
	}
	engine.cleanup()
	if _, err := os.Stat(engine.leasePath()); err != nil {
		t.Fatalf("other owner lease removed: %v", err)
	}
}

func TestRejectsUnsafeStateTargets(t *testing.T) {
	root := t.TempDir()
	target := privateStateDir(t)
	link := filepath.Join(root, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	clock := &testClock{now: time.Now().UTC()}
	config := testConfig(t, link, clock, newTestTicker())
	if _, err := New(config); err == nil {
		t.Fatal("New accepted symlink state directory")
	}
	unsafe := newBroadStateDir(t, root)
	config = testConfig(t, unsafe, clock, newTestTicker())
	if _, err := New(config); err == nil {
		t.Fatal("New accepted non-private state directory")
	}
}

func TestStatusMarksExpiredLeaseStale(t *testing.T) {
	clock := &testClock{now: time.Date(2026, 7, 23, 12, 2, 0, 0, time.UTC)}
	engine, err := New(testConfig(t, privateStateDir(t), clock, newTestTicker()))
	if err != nil {
		t.Fatal(err)
	}
	if err := engine.writeLease(clock.now.Add(-2 * time.Minute)); err != nil {
		t.Fatal(err)
	}
	if got := engine.Status(context.Background()).Code; got != StatusStale {
		t.Fatalf("expired lease status = %q, want %q", got, StatusStale)
	}
}

func TestStatusRejectsSymlinkLease(t *testing.T) {
	clock := &testClock{now: time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)}
	engine, err := New(testConfig(t, privateStateDir(t), clock, newTestTicker()))
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(engine.stateDir, "target")
	if err := writePrivateTestFile(target, []byte(`{"version":1}`)); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, engine.leasePath()); err != nil {
		t.Fatal(err)
	}
	if got := engine.Status(context.Background()).Code; got != StatusUnknown {
		t.Fatalf("symlink lease status = %q, want %q", got, StatusUnknown)
	}
}

func TestWatchDoesNotUseProcessControlAPIs(t *testing.T) {
	source, err := os.ReadFile("watch.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"os.FindProcess", "syscall.Kill", ".Signal(", "exec.Command"} {
		if strings.Contains(string(source), forbidden) {
			t.Fatalf("watch source contains process-control API %q", forbidden)
		}
	}
}

func TestIndependentStatusClientUsesLeaseIdentity(t *testing.T) {
	clock := &testClock{now: time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)}
	dir := privateStateDir(t)
	owner, err := New(testConfig(t, dir, clock, newTestTicker()))
	if err != nil {
		t.Fatal(err)
	}
	if err := owner.writeLease(clock.now); err != nil {
		t.Fatal(err)
	}
	readerConfig := testConfig(t, dir, clock, newTestTicker())
	readerConfig.PID = 9876
	readerConfig.Nonce = func() (string, error) { return "fedcba9876543210fedcba9876543210", nil }
	reader, err := New(readerConfig)
	if err != nil {
		t.Fatal(err)
	}
	if got := reader.Status(context.Background()).Code; got != StatusActive {
		t.Fatalf("independent status = %q, want %q", got, StatusActive)
	}
}

func TestRunRecoversOnlyStaleCrashLease(t *testing.T) {
	clock := &testClock{now: time.Date(2026, 7, 23, 12, 2, 0, 0, time.UTC)}
	engine, err := New(testConfig(t, privateStateDir(t), clock, newTestTicker()))
	if err != nil {
		t.Fatal(err)
	}
	if err := engine.writeLease(clock.now.Add(-2 * time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := writePrivateTestFile(engine.lockPath(), []byte(engine.nonce)); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if got := engine.Run(ctx).Code; got != ResultStopped {
		t.Fatalf("recovered run = %q, want %q", got, ResultStopped)
	}
}

func TestConcurrentRecoveryHasOneLockWinner(t *testing.T) {
	clock := &testClock{now: time.Date(2026, 7, 23, 12, 2, 0, 0, time.UTC)}
	dir := privateStateDir(t)
	first, err := New(testConfig(t, dir, clock, newTestTicker()))
	if err != nil {
		t.Fatal(err)
	}
	if err := first.writeLease(clock.now.Add(-2 * time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := writePrivateTestFile(first.lockPath(), []byte(first.nonce)); err != nil {
		t.Fatal(err)
	}
	secondConfig := testConfig(t, dir, clock, newTestTicker())
	secondConfig.Nonce = func() (string, error) { return "fedcba9876543210fedcba9876543210", nil }
	second, err := New(secondConfig)
	if err != nil {
		t.Fatal(err)
	}
	results := make(chan bool, 2)
	go func() { results <- first.acquireLock(context.Background()) }()
	go func() { results <- second.acquireLock(context.Background()) }()
	wins := 0
	for range 2 {
		if <-results {
			wins++
		}
	}
	if wins != 1 {
		t.Fatalf("recovery lock winners = %d, want 1", wins)
	}
	first.cleanup()
	second.cleanup()
}

func TestRunRecoversBoundedlyFromExpiredInvalidLease(t *testing.T) {
	clock := &testClock{now: time.Date(2026, 7, 23, 12, 2, 0, 0, time.UTC)}
	engine, err := New(testConfig(t, privateStateDir(t), clock, newTestTicker()))
	if err != nil {
		t.Fatal(err)
	}
	if err := writePrivateTestFile(engine.lockPath(), []byte(engine.nonce)); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(engine.lockPath(), clock.now.Add(-2*time.Minute), clock.now.Add(-2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := writePrivateTestFile(engine.leasePath(), []byte("invalid")); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if got := engine.Run(ctx).Code; got != ResultStopped {
		t.Fatalf("bounded recovery run = %q, want %q", got, ResultStopped)
	}
}

func TestNilContextAndInvalidTickerIntervalFailSafely(t *testing.T) {
	clock := &testClock{now: time.Now().UTC()}
	config := testConfig(t, privateStateDir(t), clock, newTestTicker())
	config.TTL = time.Nanosecond
	if _, err := New(config); err == nil {
		t.Fatal("New accepted too-small TTL")
	}
	engine, err := New(testConfig(t, privateStateDir(t), clock, newTestTicker()))
	if err != nil {
		t.Fatal(err)
	}
	if got := engine.Status(nil).Code; got != StatusUnknown {
		t.Fatalf("nil status = %q", got)
	}
	if got := engine.Run(nil).Code; got != ResultFailed {
		t.Fatalf("nil run = %q", got)
	}
	config = testConfig(t, privateStateDir(t), clock, newTestTicker())
	config.Ticker = func(time.Duration) Ticker { return nil }
	engine, err = New(config)
	if err != nil {
		t.Fatal(err)
	}
	if got := engine.Run(context.Background()).Code; got != ResultFailed {
		t.Fatalf("nil ticker result = %q", got)
	}
}
