// Package agentinstall installs and inspects OMG's global agent-discovery surfaces.
package agentinstall

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/jeremy-merchant/oh-my-group/internal/integration/instructions"
)

const (
	managedSkillMarker = "<!-- OMG GLOBAL SKILL v1 -->"
	managedSkillEnd    = "<!-- OMG GLOBAL SKILL END v1 -->"
)

var (
	ErrUnsafeHome = errors.New("unsafe agent home")
	ErrConflict   = errors.New("agent integration conflicts with existing content")
	ErrIO         = errors.New("agent integration I/O failure")
)

type State string

const (
	StateInstalled State = "installed"
	StateMissing   State = "missing"
	StateDrifted   State = "drifted"
	StateUnsafe    State = "unsafe"
)

type Surface struct {
	Provider string `json:"provider"`
	Kind     string `json:"kind"`
	Path     string `json:"path"`
	State    State  `json:"state"`
	Detected bool   `json:"detected"`
	Action   string `json:"action,omitempty"`
}

type Summary struct {
	Installed int `json:"installed"`
	Missing   int `json:"missing"`
	Drifted   int `json:"drifted"`
	Unsafe    int `json:"unsafe"`
	Detected  int `json:"detected"`
}

type Report struct {
	Status   string    `json:"status"`
	Home     string    `json:"home"`
	Surfaces []Surface `json:"surfaces"`
	Summary  Summary   `json:"summary"`
}

type surfaceSpec struct {
	provider   string
	kind       string
	path       string
	executable string
}

var instructionSpecs = []surfaceSpec{
	{provider: "Claude", kind: "instructions", path: ".claude/CLAUDE.md", executable: "claude"},
	{provider: "Codex", kind: "instructions", path: ".codex/AGENTS.md", executable: "codex"},
	{provider: "Gemini", kind: "instructions", path: ".gemini/GEMINI.md", executable: "gemini"},
	{provider: "Cursor", kind: "rule", path: ".cursor/rules/omg.mdc", executable: "cursor"},
	{provider: "Windsurf", kind: "rule", path: ".windsurf/rules/omg.md", executable: "windsurf"},
	{provider: "Cline", kind: "rule", path: ".cline/rules/omg.md", executable: "cline"},
	{provider: "OpenCode", kind: "instructions", path: ".config/opencode/AGENTS.md", executable: "opencode"},
}

var skillSpecs = []surfaceSpec{
	{provider: "Agents", kind: "skill", path: ".agents/skills/omg/SKILL.md"},
	{provider: "Claude", kind: "skill", path: ".claude/skills/omg/SKILL.md", executable: "claude"},
	{provider: "Codex", kind: "skill", path: ".codex/skills/omg/SKILL.md", executable: "codex"},
	{provider: "Gemini", kind: "skill", path: ".gemini/skills/omg/SKILL.md", executable: "gemini"},
	{provider: "OMP", kind: "skill", path: ".omp/agent/skills/omg/SKILL.md", executable: "omp"},
}

type Service struct {
	home     string
	lookPath func(string) (string, error)
}

func New(home string) (*Service, error) {
	if strings.TrimSpace(home) == "" {
		resolved, err := os.UserHomeDir()
		if err != nil {
			return nil, ErrUnsafeHome
		}
		home = resolved
	}
	absolute, err := filepath.Abs(home)
	if err != nil {
		return nil, ErrUnsafeHome
	}
	info, err := os.Lstat(absolute)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, ErrUnsafeHome
	}
	canonical, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return nil, ErrUnsafeHome
	}
	return &Service{home: canonical, lookPath: exec.LookPath}, nil
}

func FromEnvironment() (*Service, error) {
	return New(os.Getenv("OMG_AGENT_HOME"))
}

func InstructionContent() string {
	return strings.Join([]string{
		"OMG is an integration and release boundary ledger, not a step-by-step development execution tool.",
		"Apply OMG only to mutation or coordination inside the selected repository. Host-level setup, account login, and unrelated package maintenance are not OMG project work.",
		"The controller selects OBSERVE, WORK_LITE, or FULL and supplies complete executable OMG commands. A worker must not inspect OMG help, examples, or payload schemas, and must not decompose a supplied command into lower-level ledger calls.",
		"OBSERVE covers read-only diagnosis, log inspection, review, and status lookup. It creates no session, task, run, reservation, progress, handoff, or preflight record.",
		"WORK_LITE covers one branch, one worker, and one bounded change. Execute one controller-supplied start command before mutation and one controller-supplied finish command after verification. Between those boundaries use normal development tools and native Git; do not emit intermediate OMG progress, heartbeat, message, or reservation commands by default.",
		"FULL is required for multiple workers or candidates, shared rolling ownership, cross-session handoff, exact-SHA integration or Canary, deploy or release work, database changes, authentication, authorization, or payment. FULL retains the complete review and release lifecycle.",
		"Git is the source of truth for SHA, clean-tree state, diff, branch and worktree identity, and reachability. OMG may record or project those facts but never replaces Git.",
		"For WORK_LITE and FULL, execute the controller-supplied preflight once. Stop when preflight execution fails, healthy is false, or mutation_allowed is false. An ownership_conflict is always a hard block.",
		"git_risks and housekeeping counts such as stale_sessions, runtime_unobservable_sessions, finished_unclosed_sessions, and integration_queue remain visible warnings or reconciliation debt; they do not block otherwise safe new work solely because the counts are nonzero.",
		"A non-safety OMG read, history, or reconciliation error is a warning: record it and continue the code workflow without command-discovery or retry loops. Never bypass a failed preflight or ownership conflict.",
		"Historical session closure and ledger reconciliation run as separate controller operations and are never destructive startup work.",
		"Never use agent-harness health as a universal shell gate. Bootstrap and self-repair failures must not block diagnosis or unrelated host-level work.",
		"The agent performs routine OMG actions itself from controller-provided commands and never asks the human to explore or operate the ledger.",
	}, "\n") + "\n"
}

func SkillContent() []byte {
	return []byte(strings.Join([]string{
		"---",
		"name: omg",
		"description: Boundary-ledger coordination. OBSERVE is ledger-free, WORK_LITE has one start and one finish, and FULL protects integration and release.",
		"alwaysApply: true",
		"metadata:",
		"  managedBy: omg",
		"  schemaVersion: 10",
		"---",
		managedSkillMarker,
		"# OMG boundary lifecycle",
		"",
		"OMG records coordination risk at integration and release boundaries; it is not the development execution loop. The agent performs routine OMG actions from complete controller-provided commands, and the human does not explore command syntax.",
		"",
		"1. The controller selects OBSERVE, WORK_LITE, or FULL. Do not call help, example, or schema commands to rediscover a supplied workflow, and do not split a complete command into session, task, run, reservation, or progress subcommands.",
		"2. OBSERVE is read-only diagnosis, log inspection, review, and status lookup. It creates no OMG record and does not run project preflight solely to inspect.",
		"3. WORK_LITE is one branch, one worker, and one bounded mutation. Execute exactly one supplied start command before mutation and exactly one supplied finish command after tests and Git verification. Do normal RED/GREEN work between those boundaries without intermediate OMG writes by default. Escalate to FULL rather than growing WORK_LITE into a command-by-command ledger.",
		"4. FULL is mandatory for multiple workers or candidates, shared rolling ownership, cross-session handoff, exact-SHA integration or Canary, deploy or release, database changes, authentication, authorization, or payment. FULL uses independent review and the complete handoff, integration, Canary, and cleanup lifecycle.",
		"5. Git is the single source of truth for SHA, clean tree, diff, branch/worktree identity, and reachability. Use native Git for inspection and verification; OMG may record the resulting evidence but does not replace Git.",
		"6. For WORK_LITE and FULL, run the supplied preflight once. Stop if execution fails, healthy is false, or mutation_allowed is false. ownership_conflict is always blocking. Do not silently bypass those failures.",
		"7. git_risks and housekeeping values including stale_sessions, runtime_unobservable_sessions, finished_unclosed_sessions, and integration_queue are warning or reconciliation signals. Nonzero historical counts do not by themselves block safe new work.",
		"8. Treat non-safety OMG read, history, and reconciliation errors as warnings and continue the code workflow without help/schema exploration or retry loops. Historical closure and cleanup are separate controller operations, never destructive startup work.",
		"9. Never use agent-harness health as a universal shell gate. Bootstrap/self-repair failures do not block read-only diagnosis or unrelated host-level work.",
		"10. OMG never grants authority to reset, clean, check out, merge, rebase, commit, push, deploy, migrate, or delete. Those actions retain their separate approval rules.",
		managedSkillEnd,
	}, "\n") + "\n")
}

func (s *Service) Status() (Report, error) {
	surfaces := make([]Surface, 0, len(instructionSpecs)+len(skillSpecs))
	for _, spec := range instructionSpecs {
		surfaces = append(surfaces, s.inspectInstruction(spec))
	}
	for _, spec := range skillSpecs {
		surfaces = append(surfaces, s.inspectSkill(spec))
	}
	return report("status", surfaces), nil
}

func (s *Service) Doctor() (Report, error) {
	report, err := s.Status()
	if err != nil {
		return Report{}, err
	}
	report.Status = "healthy"
	if report.Summary.Missing > 0 || report.Summary.Drifted > 0 || report.Summary.Unsafe > 0 {
		report.Status = "needs_attention"
	}
	return report, nil
}

func (s *Service) Install() (Report, error) {
	for _, spec := range skillSpecs {
		state, managed := s.skillState(spec)
		if state == StateUnsafe {
			return Report{}, ErrUnsafeHome
		}
		if state == StateDrifted && !managed {
			return Report{}, ErrConflict
		}
	}
	if err := s.ensureParents(); err != nil {
		return Report{}, err
	}
	changes := make([]fileSnapshot, 0, len(skillSpecs))
	for _, spec := range skillSpecs {
		snapshot, changed, err := s.installSkill(spec)
		if err != nil {
			_ = rollbackFiles(changes)
			s.pruneEmptyParents()
			return Report{}, err
		}
		if changed {
			changes = append(changes, snapshot)
		}
	}
	targets := make([]instructions.Target, 0, len(instructionSpecs))
	for _, spec := range instructionSpecs {
		targets = append(targets, instructions.Target{Path: spec.path})
	}
	engine, err := instructions.New(s.home, targets, InstructionContent())
	if err != nil {
		_ = rollbackFiles(changes)
		s.pruneEmptyParents()
		return Report{}, mapInstructionError(err)
	}
	results, err := engine.Apply()
	if err != nil {
		_ = rollbackFiles(changes)
		s.pruneEmptyParents()
		return Report{}, mapInstructionError(err)
	}
	actions := make(map[string]string, len(results))
	for _, result := range results {
		actions[filepath.Clean(result.Target.Path)] = string(result.Action)
	}
	report, err := s.Status()
	if err != nil {
		return Report{}, err
	}
	report.Status = "installed"
	for index := range report.Surfaces {
		path := strings.TrimPrefix(report.Surfaces[index].Path, "~/")
		if action := actions[filepath.Clean(path)]; action != "" {
			report.Surfaces[index].Action = action
		} else if report.Surfaces[index].Kind == "skill" {
			report.Surfaces[index].Action = "installed"
		}
	}
	return report, nil
}

func (s *Service) Uninstall() (Report, error) {
	for _, spec := range skillSpecs {
		state, managed := s.skillState(spec)
		if state == StateUnsafe {
			return Report{}, ErrUnsafeHome
		}
		if state == StateDrifted && !managed {
			return Report{}, ErrConflict
		}
	}
	removed := make([]fileSnapshot, 0, len(skillSpecs))
	for _, spec := range skillSpecs {
		snapshot, changed, err := s.removeSkill(spec)
		if err != nil {
			_ = rollbackFiles(removed)
			return Report{}, err
		}
		if changed {
			removed = append(removed, snapshot)
		}
	}
	targets := make([]instructions.Target, 0, len(instructionSpecs))
	for _, spec := range instructionSpecs {
		targets = append(targets, instructions.Target{Path: spec.path})
	}
	engine, err := instructions.New(s.home, targets, InstructionContent())
	if err != nil {
		_ = rollbackFiles(removed)
		return Report{}, mapInstructionError(err)
	}
	if _, err := engine.Remove(); err != nil {
		_ = rollbackFiles(removed)
		return Report{}, mapInstructionError(err)
	}
	s.pruneEmptyParents()
	report, err := s.Status()
	if err != nil {
		return Report{}, err
	}
	report.Status = "uninstalled"
	for index := range report.Surfaces {
		report.Surfaces[index].Action = "removed"
	}
	return report, nil
}

func (s *Service) inspectInstruction(spec surfaceSpec) Surface {
	state := s.inspectInstructionText(spec.path)
	return Surface{Provider: spec.provider, Kind: spec.kind, Path: displayPath(spec.path), State: state, Detected: s.detected(spec.executable)}
}

func (s *Service) inspectInstructionText(relative string) State {
	path, safe := s.safeExistingPath(relative)
	if !safe {
		return StateUnsafe
	}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return StateMissing
	}
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return StateUnsafe
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return StateUnsafe
	}
	text := string(data)
	begin, end := "<!-- OMG BEGIN v1 -->", "<!-- OMG END v1 -->"
	if !strings.Contains(text, begin) && !strings.Contains(text, end) {
		return StateMissing
	}
	if strings.Contains(text, begin) && strings.Contains(text, end) && strings.Contains(text, InstructionContent()) {
		return StateInstalled
	}
	return StateDrifted
}

func (s *Service) inspectSkill(spec surfaceSpec) Surface {
	state, _ := s.skillState(spec)
	return Surface{Provider: spec.provider, Kind: spec.kind, Path: displayPath(spec.path), State: state, Detected: s.detected(spec.executable)}
}

func (s *Service) skillState(spec surfaceSpec) (State, bool) {
	path, safe := s.safeExistingPath(spec.path)
	if !safe {
		return StateUnsafe, false
	}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return StateMissing, false
	}
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return StateUnsafe, false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return StateUnsafe, false
	}
	if bytes.Equal(data, SkillContent()) {
		return StateInstalled, true
	}
	managed := isManagedSkill(data)
	return StateDrifted, managed
}

func isManagedSkill(data []byte) bool {
	text := string(data)
	return strings.Contains(text, "name: omg") &&
		strings.Contains(text, "managedBy: omg") &&
		strings.Contains(text, managedSkillMarker) &&
		strings.Contains(text, managedSkillEnd)
}

func (s *Service) ensureParents() error {
	seen := map[string]struct{}{}
	for _, spec := range append(append([]surfaceSpec{}, instructionSpecs...), skillSpecs...) {
		directory := filepath.Dir(spec.path)
		if _, ok := seen[directory]; ok {
			continue
		}
		seen[directory] = struct{}{}
		if err := s.ensureDirectory(directory); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) ensureDirectory(relative string) error {
	current := s.home
	for _, part := range strings.Split(filepath.Clean(relative), string(filepath.Separator)) {
		if part == "." || part == "" {
			continue
		}
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			if err := os.Mkdir(current, 0o700); err != nil {
				return ErrIO
			}
			continue
		}
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return ErrUnsafeHome
		}
	}
	return nil
}

func (s *Service) safeExistingPath(relative string) (string, bool) {
	clean := filepath.Clean(relative)
	if clean == "." || filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", false
	}
	current := s.home
	parts := strings.Split(filepath.Dir(clean), string(filepath.Separator))
	for _, part := range parts {
		if part == "." || part == "" {
			continue
		}
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			return filepath.Join(s.home, clean), true
		}
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return "", false
		}
	}
	return filepath.Join(s.home, clean), true
}

func (s *Service) detected(executable string) bool {
	if executable == "" {
		return false
	}
	_, err := s.lookPath(executable)
	return err == nil
}

type fileSnapshot struct {
	path   string
	exists bool
	data   []byte
	mode   fs.FileMode
}

func (s *Service) installSkill(spec surfaceSpec) (fileSnapshot, bool, error) {
	path, safe := s.safeExistingPath(spec.path)
	if !safe {
		return fileSnapshot{}, false, ErrUnsafeHome
	}
	snapshot, err := snapshotFile(path)
	if err != nil {
		return fileSnapshot{}, false, err
	}
	if snapshot.exists && bytes.Equal(snapshot.data, SkillContent()) {
		return snapshot, false, nil
	}
	if snapshot.exists && !isManagedSkill(snapshot.data) {
		return fileSnapshot{}, false, ErrConflict
	}
	mode := fs.FileMode(0o600)
	if snapshot.exists {
		mode = snapshot.mode
	}
	if err := atomicReplace(path, SkillContent(), mode, snapshot); err != nil {
		return fileSnapshot{}, false, err
	}
	return snapshot, true, nil
}

func (s *Service) removeSkill(spec surfaceSpec) (fileSnapshot, bool, error) {
	path, safe := s.safeExistingPath(spec.path)
	if !safe {
		return fileSnapshot{}, false, ErrUnsafeHome
	}
	snapshot, err := snapshotFile(path)
	if err != nil {
		return fileSnapshot{}, false, err
	}
	if !snapshot.exists {
		return snapshot, false, nil
	}
	if !isManagedSkill(snapshot.data) {
		return fileSnapshot{}, false, ErrConflict
	}
	if err := os.Remove(path); err != nil {
		return fileSnapshot{}, false, ErrIO
	}
	return snapshot, true, nil
}

func snapshotFile(path string) (fileSnapshot, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return fileSnapshot{path: path}, nil
	}
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return fileSnapshot{}, ErrUnsafeHome
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return fileSnapshot{}, ErrIO
	}
	return fileSnapshot{path: path, exists: true, data: data, mode: info.Mode().Perm()}, nil
}

func atomicReplace(path string, data []byte, mode fs.FileMode, expected fileSnapshot) error {
	actual, err := snapshotFile(path)
	if err != nil {
		return err
	}
	if actual.exists != expected.exists || actual.mode != expected.mode || !bytes.Equal(actual.data, expected.data) {
		return ErrConflict
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".omg-agent-*")
	if err != nil {
		return ErrIO
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return ErrIO
	}
	if _, err := temporary.Write(data); err != nil {
		temporary.Close()
		return ErrIO
	}
	if err := temporary.Chmod(mode); err != nil {
		temporary.Close()
		return ErrIO
	}
	if err := temporary.Close(); err != nil {
		return ErrIO
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return ErrIO
	}
	return nil
}

func rollbackFiles(changes []fileSnapshot) error {
	var combined error
	for index := len(changes) - 1; index >= 0; index-- {
		snapshot := changes[index]
		if snapshot.exists {
			current, err := snapshotFile(snapshot.path)
			if err != nil {
				combined = errors.Join(combined, err)
				continue
			}
			if err := atomicReplace(snapshot.path, snapshot.data, snapshot.mode, current); err != nil {
				combined = errors.Join(combined, err)
			}
			continue
		}
		if err := os.Remove(snapshot.path); err != nil && !errors.Is(err, os.ErrNotExist) {
			combined = errors.Join(combined, ErrIO)
		}
	}
	return combined
}

func (s *Service) pruneEmptyParents() {
	directories := make([]string, 0, len(instructionSpecs)+len(skillSpecs))
	for _, spec := range append(append([]surfaceSpec{}, instructionSpecs...), skillSpecs...) {
		directory := filepath.Dir(filepath.Join(s.home, spec.path))
		for directory != s.home && strings.HasPrefix(directory, s.home+string(filepath.Separator)) {
			directories = append(directories, directory)
			directory = filepath.Dir(directory)
		}
	}
	sort.Slice(directories, func(i, j int) bool { return len(directories[i]) > len(directories[j]) })
	seen := map[string]struct{}{}
	for _, directory := range directories {
		if _, ok := seen[directory]; ok {
			continue
		}
		seen[directory] = struct{}{}
		_ = os.Remove(directory)
	}
}

func mapInstructionError(err error) error {
	switch {
	case errors.Is(err, instructions.ErrUnsafeTarget), errors.Is(err, instructions.ErrMalformedBlock), errors.Is(err, instructions.ErrUnsupportedEncoding):
		return ErrConflict
	case errors.Is(err, instructions.ErrChanged):
		return ErrConflict
	default:
		return ErrIO
	}
}

func report(status string, surfaces []Surface) Report {
	summary := Summary{}
	detectedProviders := map[string]struct{}{}
	for _, surface := range surfaces {
		if surface.Detected {
			detectedProviders[surface.Provider] = struct{}{}
		}
		switch surface.State {
		case StateInstalled:
			summary.Installed++
		case StateMissing:
			summary.Missing++
		case StateDrifted:
			summary.Drifted++
		case StateUnsafe:
			summary.Unsafe++
		}
	}
	summary.Detected = len(detectedProviders)
	return Report{Status: status, Home: "~", Surfaces: surfaces, Summary: summary}
}

func displayPath(relative string) string {
	return "~/" + filepath.ToSlash(relative)
}

func (report Report) Validate() error {
	if report.Status == "" || report.Home != "~" || len(report.Surfaces) == 0 {
		return fmt.Errorf("invalid agent integration report")
	}
	return nil
}
