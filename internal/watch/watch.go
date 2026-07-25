// Package watch provides an optional, foreground-only refresh lifecycle.
package watch

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"time"
)

const (
	leaseFileName        = ".watch-status"
	lockFileName         = ".watch-lock"
	recoveryLockFileName = ".watch-recovery-lock"
	maxLeaseBytes        = 512
)

var ErrInvalidConfig = errors.New("invalid watch configuration")

// Clock supplies time for deterministic lifecycle evaluation.
type Clock interface{ Now() time.Time }

// Ticker supplies periodic refresh events. It never creates a background service.
type Ticker interface {
	Ticks() <-chan time.Time
	Stop()
}

// ProcessObserver observes a process identity without control over that process.
// A match must establish the supplied per-run nonce, not merely a reusable PID.
type ProcessObserver interface {
	Observe(context.Context, int, string) Observation
}

type Observation string

const (
	ObservationMatch    Observation = "match"
	ObservationMismatch Observation = "mismatch"
	ObservationUnknown  Observation = "unknown"
)

type StatusCode string

const (
	StatusDisabled StatusCode = "disabled"
	StatusStopped  StatusCode = "stopped"
	StatusActive   StatusCode = "active"
	StatusStale    StatusCode = "stale"
	StatusUnknown  StatusCode = "unknown"
)

// Status contains only a stable lifecycle code; it intentionally exposes no PID,
// nonce, filesystem location, or observer diagnostics.
type Status struct {
	Code StatusCode `json:"code"`
}

type ResultCode string

const (
	ResultStopped  ResultCode = "stopped"
	ResultConflict ResultCode = "conflict"
	ResultFailed   ResultCode = "failed"
)

type CallbackCode string

const (
	CallbackOK     CallbackCode = "ok"
	CallbackFailed CallbackCode = "failed"
)

// Callback invokes adapter-level refresh work. Its error is never exposed by Run.
type Callback func(context.Context) error

// Result reports only safe lifecycle and callback outcome codes.
type Result struct {
	Code      ResultCode     `json:"code"`
	Callbacks []CallbackCode `json:"callbacks"`
}

// Config supplies all environment-dependent behavior explicitly.
type Config struct {
	StateDir  string
	PID       int
	TTL       time.Duration
	Clock     Clock
	Ticker    func(time.Duration) Ticker
	Observer  ProcessObserver
	Callbacks []Callback
	Nonce     func() (string, error)
}

type lease struct {
	Version   int    `json:"version"`
	PID       int    `json:"pid"`
	Nonce     string `json:"nonce"`
	Heartbeat string `json:"heartbeat"`
}

// Engine owns one foreground watch invocation.
type Engine struct {
	stateDir  string
	pid       int
	ttl       time.Duration
	clock     Clock
	ticker    func(time.Duration) Ticker
	observer  ProcessObserver
	callbacks []Callback
	nonce     string
}

func New(config Config) (*Engine, error) {
	if config.PID <= 0 || config.TTL < 2*time.Nanosecond || config.Clock == nil || config.Ticker == nil || config.Observer == nil || config.Nonce == nil {
		return nil, ErrInvalidConfig
	}
	if err := validateStateDir(config.StateDir); err != nil {
		return nil, ErrInvalidConfig
	}
	nonce, err := config.Nonce()
	if err != nil || !validNonce(nonce) {
		return nil, ErrInvalidConfig
	}
	callbacks := append([]Callback(nil), config.Callbacks...)
	return &Engine{stateDir: config.StateDir, pid: config.PID, ttl: config.TTL, clock: config.Clock, ticker: config.Ticker, observer: config.Observer, callbacks: callbacks, nonce: nonce}, nil
}

// RandomNonce produces a cryptographically random per-run nonce suitable for Config.Nonce.
func RandomNonce() (string, error) {
	var raw [16]byte
	if _, err := io.ReadFull(rand.Reader, raw[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw[:]), nil
}

// Status evaluates a lease conservatively. It never infers liveness from PID alone.
func (e *Engine) Status(ctx context.Context) Status {
	if ctx == nil {
		return Status{Code: StatusUnknown}
	}
	record, exists, valid := e.readLease()
	if !exists {
		return Status{Code: StatusStopped}
	}
	if !valid {
		return Status{Code: StatusUnknown}
	}
	heartbeat, err := time.Parse(time.RFC3339Nano, record.Heartbeat)
	if err != nil || heartbeat.After(e.clock.Now().Add(e.ttl)) || e.clock.Now().Sub(heartbeat) > e.ttl {
		return Status{Code: StatusStale}
	}
	switch e.observer.Observe(ctx, record.PID, record.Nonce) {
	case ObservationMatch:
		return Status{Code: StatusActive}
	case ObservationMismatch:
		return Status{Code: StatusStale}
	default:
		return Status{Code: StatusUnknown}
	}
}

// Run keeps the adapter in the foreground until ctx is cancelled. It does not
// start a daemon, signal a process, or apply domain business logic.
func (e *Engine) Run(ctx context.Context) Result {
	if ctx == nil {
		return Result{Code: ResultFailed}
	}
	if !e.acquireLock(ctx) {
		return Result{Code: ResultConflict}
	}
	defer e.cleanup()
	if err := e.writeLease(e.clock.Now()); err != nil {
		return Result{Code: ResultFailed}
	}
	ticker := e.ticker(e.ttl / 2)
	if ticker == nil {
		return Result{Code: ResultFailed}
	}
	defer ticker.Stop()
	outcomes := make([]CallbackCode, len(e.callbacks))
	for {
		select {
		case <-ctx.Done():
			return Result{Code: ResultStopped, Callbacks: append([]CallbackCode(nil), outcomes...)}
		case <-ticker.Ticks():
			if err := e.writeLease(e.clock.Now()); err != nil {
				return Result{Code: ResultFailed, Callbacks: append([]CallbackCode(nil), outcomes...)}
			}
			for i, callback := range e.callbacks {
				outcomes[i] = invoke(ctx, callback)
			}
		}
	}
}

func invoke(ctx context.Context, callback Callback) (code CallbackCode) {
	code = CallbackOK
	defer func() {
		if recover() != nil {
			code = CallbackFailed
		}
	}()
	if callback == nil || callback(ctx) != nil {
		return CallbackFailed
	}
	return code
}

func (e *Engine) leasePath() string { return filepath.Join(e.stateDir, leaseFileName) }
func (e *Engine) lockPath() string  { return filepath.Join(e.stateDir, lockFileName) }
func (e *Engine) recoveryLockPath() string {
	return filepath.Join(e.stateDir, recoveryLockFileName)
}

func (e *Engine) acquireLock(ctx context.Context) bool {
	release, ok := acquireRecoveryGuard(e.recoveryLockPath())
	if !ok {
		return false
	}
	defer release()

	if e.createLock() {
		return e.initializeLock()
	}
	if !e.reclaimable(ctx) {
		return false
	}
	if record, exists, valid := e.readLease(); exists && valid {
		heartbeat, err := time.Parse(time.RFC3339Nano, record.Heartbeat)
		if err == nil && (e.clock.Now().Sub(heartbeat) > e.ttl || e.observer.Observe(ctx, record.PID, record.Nonce) == ObservationMismatch) {
			e.removeLeaseNonce(record.Nonce)
		}
	} else if exists {
		e.removeInvalidLeaseIfPrivate()
	}
	if err := os.Remove(e.lockPath()); err != nil || !e.createLock() {
		return false
	}
	return e.initializeLock()
}

func (e *Engine) initializeLock() bool {
	if err := e.writeLease(e.clock.Now()); err != nil {
		e.removeIfOwned(e.lockPath())
		return false
	}
	return true
}

func (e *Engine) createLock() bool {
	file, err := os.OpenFile(e.lockPath(), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return false
	}
	if err := secureNewPrivateFile(e.lockPath()); err != nil {
		_ = file.Close()
		_ = os.Remove(e.lockPath())
		return false
	}
	_, writeErr := file.WriteString(e.nonce)
	closeErr := file.Close()
	if writeErr != nil || closeErr != nil {
		_ = os.Remove(e.lockPath())
		return false
	}
	return true
}

func (e *Engine) reclaimable(ctx context.Context) bool {
	lockInfo, err := os.Lstat(e.lockPath())
	if err != nil || !validatePrivateRegularFile(e.lockPath(), lockInfo) {
		return false
	}
	record, exists, valid := e.readLease()
	if !exists || !valid {
		return e.clock.Now().Sub(lockInfo.ModTime()) > e.ttl
	}
	heartbeat, err := time.Parse(time.RFC3339Nano, record.Heartbeat)
	if err != nil {
		return false
	}
	if e.clock.Now().Sub(heartbeat) > e.ttl {
		return true
	}
	return e.observer.Observe(ctx, record.PID, record.Nonce) == ObservationMismatch
}

func (e *Engine) writeLease(now time.Time) error {
	if existing, exists, valid := e.readLease(); exists && (!valid || existing.Nonce != e.nonce) {
		return ErrInvalidConfig
	}
	payload, err := json.Marshal(lease{Version: 1, PID: e.pid, Nonce: e.nonce, Heartbeat: now.UTC().Format(time.RFC3339Nano)})
	if err != nil || len(payload) > maxLeaseBytes {
		return ErrInvalidConfig
	}
	return atomicWrite(e.leasePath(), payload)
}

func (e *Engine) readLease() (lease, bool, bool) {
	info, err := os.Lstat(e.leasePath())
	if errors.Is(err, os.ErrNotExist) {
		return lease{}, false, false
	}
	if err != nil || !validatePrivateRegularFile(e.leasePath(), info) {
		return lease{}, true, false
	}
	file, err := os.Open(e.leasePath())
	if err != nil {
		return lease{}, true, false
	}
	defer file.Close()
	openedInfo, statErr := file.Stat()
	if statErr != nil || !validatePrivateRegularFile(e.leasePath(), openedInfo) || !os.SameFile(info, openedInfo) {
		return lease{}, true, false
	}
	data, err := io.ReadAll(io.LimitReader(file, maxLeaseBytes+1))
	if err != nil || len(data) == 0 || len(data) > maxLeaseBytes {
		return lease{}, true, false
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var record lease
	if decoder.Decode(&record) != nil || decoder.Decode(&struct{}{}) != io.EOF || record.Version != 1 || record.PID <= 0 || !validNonce(record.Nonce) || record.Heartbeat == "" {
		return lease{}, true, false
	}
	return record, true, true
}

func (e *Engine) cleanup() {
	e.removeIfOwned(e.leasePath())
	e.removeIfOwned(e.lockPath())
}

func (e *Engine) removeIfOwned(path string) {
	info, err := os.Lstat(path)
	if err != nil || !validatePrivateRegularFile(path, info) {
		return
	}
	data, err := os.ReadFile(path)
	if err == nil && string(data) == e.nonce {
		_ = os.Remove(path)
		return
	}
	if path == e.leasePath() {
		record, _, valid := e.readLease()
		if valid && record.Nonce == e.nonce {
			_ = os.Remove(path)
		}
	}
}

func (e *Engine) removeLeaseNonce(nonce string) {
	record, _, valid := e.readLease()
	if valid && record.Nonce == nonce {
		_ = os.Remove(e.leasePath())
	}
}

func (e *Engine) removeInvalidLeaseIfPrivate() {
	info, err := os.Lstat(e.leasePath())
	if err == nil && validatePrivateRegularFile(e.leasePath(), info) {
		_ = os.Remove(e.leasePath())
	}
}
func atomicWrite(path string, data []byte) error {
	dir := filepath.Dir(path)
	temp, err := os.CreateTemp(dir, ".watch-tmp-")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if err := secureNewPrivateFile(tempPath); err != nil {
		temp.Close()
		return err
	}
	if _, err := temp.Write(data); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(tempPath, path)
}

func validNonce(nonce string) bool {
	if len(nonce) != 32 {
		return false
	}
	_, err := hex.DecodeString(nonce)
	return err == nil
}

type wallClock struct{}

func (wallClock) Now() time.Time { return time.Now() }

type wallTicker struct{ ticker *time.Ticker }

func (t wallTicker) Ticks() <-chan time.Time { return t.ticker.C }
func (t wallTicker) Stop()                   { t.ticker.Stop() }

type conservativeProcessObserver struct{}

func (conservativeProcessObserver) Observe(context.Context, int, string) Observation {
	return ObservationUnknown
}

// NewSystem constructs a foreground engine from local OS primitives. Portable
// process observation cannot prove a per-run nonce, so external status checks
// remain conservatively unknown rather than trusting a reused PID.
func NewSystem(stateDir string, ttl time.Duration, callbacks []Callback) (*Engine, error) {
	return New(Config{
		StateDir: stateDir,
		PID:      os.Getpid(),
		TTL:      ttl,
		Clock:    wallClock{},
		Ticker: func(interval time.Duration) Ticker {
			return wallTicker{ticker: time.NewTicker(interval)}
		},
		Observer:  conservativeProcessObserver{},
		Callbacks: callbacks,
		Nonce:     RandomNonce,
	})
}
