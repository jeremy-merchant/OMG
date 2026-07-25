// Package config loads safe project configuration.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jeremy-merchant/OMG/internal/safety"
	"github.com/pelletier/go-toml/v2"
)

const (
	projectConfig = ".omg/project.toml"
	localConfig   = ".omg/local.toml"
)

var (
	secretLike       = regexp.MustCompile(`(?i)(token|secret|password|credential|api[_-]?key|bearer\s|-----begin)`)
	projectName      = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)
	taskPrefix       = regexp.MustCompile(`^[A-Z][A-Z0-9_-]*$`)
	managedMarker    = regexp.MustCompile(`^[a-z][a-z0-9_-]*$`)
	allowedOMGEnvKey = map[string]struct{}{
		"OMG_WORKSPACE": {}, "OMG_STORE_PATH": {},
		"OMG_PROJECT_NAME": {}, "OMG_PROJECT_DISPLAY": {}, "OMG_PROJECT_TASK_PREFIX": {}, "OMG_PROJECT_DEFAULT_BRANCH": {},
		"OMG_COORDINATION_MAX_DELEGATION_DEPTH": {}, "OMG_COORDINATION_TASK_CLAIM_MODE": {}, "OMG_COORDINATION_DEFAULT_DEPENDENCY_UNBLOCK_ON": {}, "OMG_COORDINATION_SEMANTIC_STALE_AFTER": {}, "OMG_COORDINATION_PROCESS_STALE_AFTER": {}, "OMG_COORDINATION_AGENT_DEAD_AFTER": {},
		"OMG_RESERVATION_DEFAULT_TTL": {}, "OMG_RESERVATION_MODE": {}, "OMG_RESERVATION_RELEASE_ON_HANDOFF": {}, "OMG_RESERVATION_RENEW_ON_CHECKPOINT": {},
		"OMG_PRIVACY_PROMPT_STORAGE": {}, "OMG_PRIVACY_FINAL_OUTPUT_STORAGE": {}, "OMG_PRIVACY_BOARD_SHOW_PROMPT": {}, "OMG_PRIVACY_EXPORT_SENSITIVE_DEFAULT": {},
		"OMG_GIT_SCAN_INTERVAL": {}, "OMG_GIT_INCLUDE_UNTRACKED_COUNT": {}, "OMG_GIT_DETECT_UNPUSHED": {}, "OMG_GIT_CLEANUP_MODE": {},
		"OMG_BOARD_DEFAULT_FORMAT": {}, "OMG_BOARD_HTML_SELF_CONTAINED": {},
		"OMG_INTEGRATIONS_MANAGED_MARKER": {},
	}
)

// TaskClaimMode controls task claim ownership.
type TaskClaimMode string

const (
	TaskClaimModeExclusive TaskClaimMode = "exclusive"
	TaskClaimModeAdvisory  TaskClaimMode = "advisory"
)

// UnblockPolicy controls the prerequisite outcome needed to unblock a task.
type UnblockPolicy string

const (
	UnblockWhenWorkComplete UnblockPolicy = "work_complete"
	UnblockWhenVerifiedDone UnblockPolicy = "verified_done"
)

// ReservationMode controls reservation ownership.
type ReservationMode string

const (
	ReservationModeAdvisory  ReservationMode = "advisory"
	ReservationModeExclusive ReservationMode = "exclusive"
)

// StorageMode controls persistence of sensitive prompt material.
type StorageMode string

const (
	StorageModeRedacted StorageMode = "redacted"
	StorageModeFull     StorageMode = "full"
)

// BoardShowPrompt controls prompt detail shown on generated boards.
type BoardShowPrompt string

const (
	BoardShowPromptSummary BoardShowPrompt = "summary"
	BoardShowPromptFull    BoardShowPrompt = "full"
)

// CleanupMode controls Git cleanup behavior.
type CleanupMode string

const (
	CleanupModePlanOnly  CleanupMode = "plan_only"
	CleanupModeAutomatic CleanupMode = "automatic"
)

// BoardFormat is a supported generated board representation.
type BoardFormat string

const (
	BoardFormatTTY      BoardFormat = "tty"
	BoardFormatJSON     BoardFormat = "json"
	BoardFormatMarkdown BoardFormat = "markdown"
	BoardFormatHTML     BoardFormat = "html"
)

// ProjectSettings describes the project identity safe to track in source.
type ProjectSettings struct {
	Name          string
	Display       string
	TaskPrefix    string
	DefaultBranch string
}

// CoordinationSettings controls safe local coordination behavior.
type CoordinationSettings struct {
	MaxDelegationDepth         int
	TaskClaimMode              TaskClaimMode
	DefaultDependencyUnblockOn UnblockPolicy
	SemanticStaleAfter         time.Duration
	ProcessStaleAfter          time.Duration
	AgentDeadAfter             time.Duration
}

// ReservationSettings controls advisory reservation behavior.
type ReservationSettings struct {
	DefaultTTL        time.Duration
	Mode              ReservationMode
	ReleaseOnHandoff  bool
	RenewOnCheckpoint bool
}

// PrivacySettings controls conservative storage and rendering defaults.
type PrivacySettings struct {
	PromptStorage          StorageMode
	FinalOutputStorage     StorageMode
	BoardShowPrompt        BoardShowPrompt
	ExportSensitiveDefault bool
}

// GitSettings controls non-destructive Git reconciliation.
type GitSettings struct {
	ScanInterval          time.Duration
	IncludeUntrackedCount bool
	DetectUnpushed        bool
	CleanupMode           CleanupMode
}

// BoardSettings controls generated board rendering.
type BoardSettings struct {
	DefaultFormat     BoardFormat
	HTMLSelfContained bool
}

// IntegrationSettings identifies OMG-managed instruction blocks and their
// project-relative runtime instruction surfaces.
type IntegrationSettings struct {
	ManagedMarker      string
	InstructionTargets []string
}

// Project is safe configuration collected from a project root. The tracked
// file is deliberately restricted to values that are safe to commit.
type Project struct {
	tracked configFile
	local   configFile
}

// ProjectOverrides represents explicitly supplied project values. Pointer
// fields preserve the distinction between an omitted override and zero/false.
type ProjectOverrides struct {
	Name          *string `toml:"name"`
	Display       *string `toml:"display"`
	TaskPrefix    *string `toml:"task_prefix"`
	DefaultBranch *string `toml:"default_branch"`
}

type CoordinationOverrides struct {
	MaxDelegationDepth         *int    `toml:"max_delegation_depth"`
	TaskClaimMode              *string `toml:"task_claim_mode"`
	DefaultDependencyUnblockOn *string `toml:"default_dependency_unblock_on"`
	SemanticStaleAfter         *string `toml:"semantic_stale_after"`
	ProcessStaleAfter          *string `toml:"process_stale_after"`
	AgentDeadAfter             *string `toml:"agent_dead_after"`
}

type ReservationOverrides struct {
	DefaultTTL        *string `toml:"default_ttl"`
	Mode              *string `toml:"mode"`
	ReleaseOnHandoff  *bool   `toml:"release_on_handoff"`
	RenewOnCheckpoint *bool   `toml:"renew_on_checkpoint"`
}

type PrivacyOverrides struct {
	PromptStorage          *string `toml:"prompt_storage"`
	FinalOutputStorage     *string `toml:"final_output_storage"`
	BoardShowPrompt        *string `toml:"board_show_prompt"`
	ExportSensitiveDefault *bool   `toml:"export_sensitive_default"`
}

type GitOverrides struct {
	ScanInterval          *string `toml:"scan_interval"`
	IncludeUntrackedCount *bool   `toml:"include_untracked_count"`
	DetectUnpushed        *bool   `toml:"detect_unpushed"`
	CleanupMode           *string `toml:"cleanup_mode"`
}

type BoardOverrides struct {
	DefaultFormat     *string `toml:"default_format"`
	HTMLSelfContained *bool   `toml:"html_self_contained"`
}

type IntegrationOverrides struct {
	ManagedMarker      *string   `toml:"managed_marker"`
	InstructionTargets *[]string `toml:"instruction_targets"`
}

// Overrides contains only safe, explicit configuration overrides.
type Overrides struct {
	Workspace    *string                `toml:"workspace"`
	Project      *ProjectOverrides      `toml:"project"`
	Coordination *CoordinationOverrides `toml:"coordination"`
	Reservation  *ReservationOverrides  `toml:"reservation"`
	Privacy      *PrivacyOverrides      `toml:"privacy"`
	Git          *GitOverrides          `toml:"git"`
	Board        *BoardOverrides        `toml:"board"`
	Integrations *IntegrationOverrides  `toml:"integrations"`
}

type configFile = Overrides

// Inputs are process-level values. Explicit fields come from the CLI;
// Environment is injectable so callers need not mutate process state in tests.
type Inputs struct {
	Workspace   string
	StorePath   string
	Environment map[string]string
	Override    *Overrides
}

// Resolved is the fully typed configuration. Workspace and StorePath preserve
// the original platform selection API.
type Resolved struct {
	Workspace    string
	StorePath    string
	Project      ProjectSettings
	Coordination CoordinationSettings
	Reservation  ReservationSettings
	Privacy      PrivacySettings
	Git          GitSettings
	Board        BoardSettings
	Integrations IntegrationSettings
}

// Load reads tracked project configuration and its untracked local override.
func Load(root string) (Project, error) {
	tracked, err := read(root, projectConfig, true)
	if err != nil {
		return Project{}, err
	}
	local, err := read(root, localConfig, false)
	if err != nil {
		return Project{}, err
	}
	return Project{tracked: tracked, local: local}, nil
}

// Resolve applies explicit CLI, safe environment, local override, tracked
// configuration, then typed defaults.
func (p Project) Resolve(in Inputs) (Resolved, error) {
	if err := rejectSecretLike(in.Workspace); err != nil {
		return Resolved{}, err
	}
	if err := rejectSecretLike(in.StorePath); err != nil {
		return Resolved{}, err
	}
	explicit := configFile{}
	if in.Override != nil {
		explicit = *in.Override
	}
	if in.Workspace != "" {
		explicit.Workspace = new(in.Workspace)
	}
	environment, err := environmentOverrides(in.Environment)
	if err != nil {
		return Resolved{}, err
	}
	if err := validateConfig(explicit); err != nil {
		return Resolved{}, err
	}
	storePath := in.StorePath
	if storePath == "" {
		storePath = in.Environment["OMG_STORE_PATH"]
	}
	if err := rejectSecretLike(storePath); err != nil {
		return Resolved{}, err
	}

	layers := []configFile{explicit, environment, p.local, p.tracked}
	resolved := Resolved{
		Workspace: pickString(layers, func(c configFile) *string { return c.Workspace }, ""),
		StorePath: storePath,
		Project: ProjectSettings{
			Name:          pickString(layers, projectNameOverride, ""),
			Display:       pickString(layers, projectDisplay, ""),
			TaskPrefix:    pickString(layers, projectTaskPrefix, "OMG"),
			DefaultBranch: pickString(layers, projectDefaultBranch, "main"),
		},
		Coordination: CoordinationSettings{
			MaxDelegationDepth:         pickInt(layers, coordinationMaxDepth, 8),
			TaskClaimMode:              TaskClaimMode(pickString(layers, coordinationTaskClaimMode, string(TaskClaimModeExclusive))),
			DefaultDependencyUnblockOn: UnblockPolicy(pickString(layers, coordinationDefaultDependencyUnblockOn, string(UnblockWhenWorkComplete))),
			SemanticStaleAfter:         pickDuration(layers, coordinationSemanticStaleAfter, 20*time.Minute),
			ProcessStaleAfter:          pickDuration(layers, coordinationProcessStaleAfter, 5*time.Minute),
			AgentDeadAfter:             pickDuration(layers, coordinationAgentDeadAfter, 2*time.Hour),
		},
		Reservation: ReservationSettings{
			DefaultTTL:        pickDuration(layers, reservationDefaultTTL, 30*time.Minute),
			Mode:              ReservationMode(pickString(layers, reservationMode, string(ReservationModeAdvisory))),
			ReleaseOnHandoff:  pickBool(layers, reservationReleaseOnHandoff, true),
			RenewOnCheckpoint: pickBool(layers, reservationRenewOnCheckpoint, true),
		},
		Privacy: PrivacySettings{
			PromptStorage:          StorageMode(pickString(layers, privacyPromptStorage, string(StorageModeRedacted))),
			FinalOutputStorage:     StorageMode(pickString(layers, privacyFinalOutputStorage, string(StorageModeRedacted))),
			BoardShowPrompt:        BoardShowPrompt(pickString(layers, privacyBoardShowPrompt, string(BoardShowPromptSummary))),
			ExportSensitiveDefault: pickBool(layers, privacyExportSensitiveDefault, false),
		},
		Git: GitSettings{
			ScanInterval:          pickDuration(layers, gitScanInterval, 5*time.Minute),
			IncludeUntrackedCount: pickBool(layers, gitIncludeUntrackedCount, true),
			DetectUnpushed:        pickBool(layers, gitDetectUnpushed, true),
			CleanupMode:           CleanupMode(pickString(layers, gitCleanupMode, string(CleanupModePlanOnly))),
		},
		Board: BoardSettings{
			DefaultFormat:     BoardFormat(pickString(layers, boardDefaultFormat, string(BoardFormatTTY))),
			HTMLSelfContained: pickBool(layers, boardHTMLSelfContained, true),
		},
		Integrations: IntegrationSettings{
			ManagedMarker:      pickString(layers, integrationsManagedMarker, "omg"),
			InstructionTargets: pickStrings(layers, integrationsInstructionTargets),
		},
	}
	if err := validateResolved(resolved); err != nil {
		return Resolved{}, err
	}
	return resolved, nil
}

func read(root, relative string, tracked bool) (configFile, error) {
	canonicalRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return configFile{}, fmt.Errorf("resolve project configuration root: %w", err)
	}
	path := filepath.Join(canonicalRoot, filepath.FromSlash(relative))
	contents, err := readConfigFile(path, !tracked)
	if errors.Is(err, os.ErrNotExist) {
		return configFile{}, nil
	}
	if err != nil {
		return configFile{}, fmt.Errorf("read project configuration: %w", err)
	}
	if safety.IsDelegationToken(string(contents)) {
		return configFile{}, errors.New("project configuration contains a delegation token")
	}
	if tracked && secretLike.Match(contents) {
		return configFile{}, errors.New("tracked project configuration contains a secret-like value")
	}
	decoder := toml.NewDecoder(bytes.NewReader(contents))
	decoder.DisallowUnknownFields()
	var parsed configFile
	if err := decoder.Decode(&parsed); err != nil {
		return configFile{}, fmt.Errorf("parse project configuration: %w", err)
	}
	if err := validateConfig(parsed); err != nil {
		return configFile{}, fmt.Errorf("validate project configuration: %w", err)
	}
	return parsed, nil
}

func environmentOverrides(environment map[string]string) (configFile, error) {
	for key := range environment {
		if strings.HasPrefix(key, "OMG_") && safety.IsDelegationToken(environment[key]) {
			return configFile{}, fmt.Errorf("OMG environment variable %q contains a delegation token", key)
		}
		if strings.HasPrefix(key, "OMG_") {
			if _, ok := allowedOMGEnvKey[key]; !ok {
				return configFile{}, fmt.Errorf("unknown OMG environment variable %q", key)
			}
		}
	}
	var result configFile
	setString := func(key string, target **string) {
		if value, ok := environment[key]; ok {
			*target = new(value)
		}
	}
	setBool := func(key string, target **bool) error {
		if value, ok := environment[key]; ok {
			parsed, err := strconv.ParseBool(value)
			if err != nil {
				return fmt.Errorf("invalid %s", key)
			}
			*target = new(parsed)
		}
		return nil
	}
	setInt := func(key string, target **int) error {
		if value, ok := environment[key]; ok {
			parsed, err := strconv.Atoi(value)
			if err != nil {
				return fmt.Errorf("invalid %s", key)
			}
			*target = new(parsed)
		}
		return nil
	}
	setString("OMG_WORKSPACE", &result.Workspace)
	if hasAny(environment, "OMG_PROJECT_NAME", "OMG_PROJECT_DISPLAY", "OMG_PROJECT_TASK_PREFIX", "OMG_PROJECT_DEFAULT_BRANCH") {
		result.Project = &ProjectOverrides{}
		setString("OMG_PROJECT_NAME", &result.Project.Name)
		setString("OMG_PROJECT_DISPLAY", &result.Project.Display)
		setString("OMG_PROJECT_TASK_PREFIX", &result.Project.TaskPrefix)
		setString("OMG_PROJECT_DEFAULT_BRANCH", &result.Project.DefaultBranch)
	}
	if hasAny(environment, "OMG_COORDINATION_MAX_DELEGATION_DEPTH", "OMG_COORDINATION_TASK_CLAIM_MODE", "OMG_COORDINATION_DEFAULT_DEPENDENCY_UNBLOCK_ON", "OMG_COORDINATION_SEMANTIC_STALE_AFTER", "OMG_COORDINATION_PROCESS_STALE_AFTER", "OMG_COORDINATION_AGENT_DEAD_AFTER") {
		result.Coordination = &CoordinationOverrides{}
		if err := setInt("OMG_COORDINATION_MAX_DELEGATION_DEPTH", &result.Coordination.MaxDelegationDepth); err != nil {
			return result, err
		}
		setString("OMG_COORDINATION_TASK_CLAIM_MODE", &result.Coordination.TaskClaimMode)
		setString("OMG_COORDINATION_DEFAULT_DEPENDENCY_UNBLOCK_ON", &result.Coordination.DefaultDependencyUnblockOn)
		setString("OMG_COORDINATION_SEMANTIC_STALE_AFTER", &result.Coordination.SemanticStaleAfter)
		setString("OMG_COORDINATION_PROCESS_STALE_AFTER", &result.Coordination.ProcessStaleAfter)
		setString("OMG_COORDINATION_AGENT_DEAD_AFTER", &result.Coordination.AgentDeadAfter)
	}
	if hasAny(environment, "OMG_RESERVATION_DEFAULT_TTL", "OMG_RESERVATION_MODE", "OMG_RESERVATION_RELEASE_ON_HANDOFF", "OMG_RESERVATION_RENEW_ON_CHECKPOINT") {
		result.Reservation = &ReservationOverrides{}
		setString("OMG_RESERVATION_DEFAULT_TTL", &result.Reservation.DefaultTTL)
		setString("OMG_RESERVATION_MODE", &result.Reservation.Mode)
		if err := setBool("OMG_RESERVATION_RELEASE_ON_HANDOFF", &result.Reservation.ReleaseOnHandoff); err != nil {
			return result, err
		}
		if err := setBool("OMG_RESERVATION_RENEW_ON_CHECKPOINT", &result.Reservation.RenewOnCheckpoint); err != nil {
			return result, err
		}
	}
	if hasAny(environment, "OMG_PRIVACY_PROMPT_STORAGE", "OMG_PRIVACY_FINAL_OUTPUT_STORAGE", "OMG_PRIVACY_BOARD_SHOW_PROMPT", "OMG_PRIVACY_EXPORT_SENSITIVE_DEFAULT") {
		result.Privacy = &PrivacyOverrides{}
		setString("OMG_PRIVACY_PROMPT_STORAGE", &result.Privacy.PromptStorage)
		setString("OMG_PRIVACY_FINAL_OUTPUT_STORAGE", &result.Privacy.FinalOutputStorage)
		setString("OMG_PRIVACY_BOARD_SHOW_PROMPT", &result.Privacy.BoardShowPrompt)
		if err := setBool("OMG_PRIVACY_EXPORT_SENSITIVE_DEFAULT", &result.Privacy.ExportSensitiveDefault); err != nil {
			return result, err
		}
	}
	if hasAny(environment, "OMG_GIT_SCAN_INTERVAL", "OMG_GIT_INCLUDE_UNTRACKED_COUNT", "OMG_GIT_DETECT_UNPUSHED", "OMG_GIT_CLEANUP_MODE") {
		result.Git = &GitOverrides{}
		setString("OMG_GIT_SCAN_INTERVAL", &result.Git.ScanInterval)
		if err := setBool("OMG_GIT_INCLUDE_UNTRACKED_COUNT", &result.Git.IncludeUntrackedCount); err != nil {
			return result, err
		}
		if err := setBool("OMG_GIT_DETECT_UNPUSHED", &result.Git.DetectUnpushed); err != nil {
			return result, err
		}
		setString("OMG_GIT_CLEANUP_MODE", &result.Git.CleanupMode)
	}
	if hasAny(environment, "OMG_BOARD_DEFAULT_FORMAT", "OMG_BOARD_HTML_SELF_CONTAINED") {
		result.Board = &BoardOverrides{}
		setString("OMG_BOARD_DEFAULT_FORMAT", &result.Board.DefaultFormat)
		if err := setBool("OMG_BOARD_HTML_SELF_CONTAINED", &result.Board.HTMLSelfContained); err != nil {
			return result, err
		}
	}
	if _, ok := environment["OMG_INTEGRATIONS_MANAGED_MARKER"]; ok {
		result.Integrations = &IntegrationOverrides{}
		setString("OMG_INTEGRATIONS_MANAGED_MARKER", &result.Integrations.ManagedMarker)
	}
	return result, validateConfig(result)
}

func hasAny(values map[string]string, keys ...string) bool {
	for _, key := range keys {
		if _, ok := values[key]; ok {
			return true
		}
	}
	return false
}
func pickString(layers []configFile, field func(configFile) *string, fallback string) string {
	for _, layer := range layers {
		if value := field(layer); value != nil {
			return *value
		}
	}
	return fallback
}
func pickInt(layers []configFile, field func(configFile) *int, fallback int) int {
	for _, layer := range layers {
		if value := field(layer); value != nil {
			return *value
		}
	}
	return fallback
}
func pickBool(layers []configFile, field func(configFile) *bool, fallback bool) bool {
	for _, layer := range layers {
		if value := field(layer); value != nil {
			return *value
		}
	}
	return fallback
}
func pickStrings(layers []configFile, field func(configFile) *[]string) []string {
	for _, layer := range layers {
		if value := field(layer); value != nil {
			return append([]string(nil), (*value)...)
		}
	}
	return nil
}
func pickDuration(layers []configFile, field func(configFile) *string, fallback time.Duration) time.Duration {
	for _, layer := range layers {
		if value := field(layer); value != nil {
			duration, _ := time.ParseDuration(*value)
			return duration
		}
	}
	return fallback
}

func projectNameOverride(c configFile) *string {
	if c.Project == nil {
		return nil
	}
	return c.Project.Name
}
func projectDisplay(c configFile) *string {
	if c.Project == nil {
		return nil
	}
	return c.Project.Display
}
func projectTaskPrefix(c configFile) *string {
	if c.Project == nil {
		return nil
	}
	return c.Project.TaskPrefix
}
func projectDefaultBranch(c configFile) *string {
	if c.Project == nil {
		return nil
	}
	return c.Project.DefaultBranch
}
func coordinationMaxDepth(c configFile) *int {
	if c.Coordination == nil {
		return nil
	}
	return c.Coordination.MaxDelegationDepth
}
func coordinationTaskClaimMode(c configFile) *string {
	if c.Coordination == nil {
		return nil
	}
	return c.Coordination.TaskClaimMode
}
func coordinationDefaultDependencyUnblockOn(c configFile) *string {
	if c.Coordination == nil {
		return nil
	}
	return c.Coordination.DefaultDependencyUnblockOn
}
func coordinationSemanticStaleAfter(c configFile) *string {
	if c.Coordination == nil {
		return nil
	}
	return c.Coordination.SemanticStaleAfter
}
func coordinationProcessStaleAfter(c configFile) *string {
	if c.Coordination == nil {
		return nil
	}
	return c.Coordination.ProcessStaleAfter
}
func coordinationAgentDeadAfter(c configFile) *string {
	if c.Coordination == nil {
		return nil
	}
	return c.Coordination.AgentDeadAfter
}
func reservationDefaultTTL(c configFile) *string {
	if c.Reservation == nil {
		return nil
	}
	return c.Reservation.DefaultTTL
}
func reservationMode(c configFile) *string {
	if c.Reservation == nil {
		return nil
	}
	return c.Reservation.Mode
}
func reservationReleaseOnHandoff(c configFile) *bool {
	if c.Reservation == nil {
		return nil
	}
	return c.Reservation.ReleaseOnHandoff
}
func reservationRenewOnCheckpoint(c configFile) *bool {
	if c.Reservation == nil {
		return nil
	}
	return c.Reservation.RenewOnCheckpoint
}
func privacyPromptStorage(c configFile) *string {
	if c.Privacy == nil {
		return nil
	}
	return c.Privacy.PromptStorage
}
func privacyFinalOutputStorage(c configFile) *string {
	if c.Privacy == nil {
		return nil
	}
	return c.Privacy.FinalOutputStorage
}
func privacyBoardShowPrompt(c configFile) *string {
	if c.Privacy == nil {
		return nil
	}
	return c.Privacy.BoardShowPrompt
}
func privacyExportSensitiveDefault(c configFile) *bool {
	if c.Privacy == nil {
		return nil
	}
	return c.Privacy.ExportSensitiveDefault
}
func gitScanInterval(c configFile) *string {
	if c.Git == nil {
		return nil
	}
	return c.Git.ScanInterval
}
func gitIncludeUntrackedCount(c configFile) *bool {
	if c.Git == nil {
		return nil
	}
	return c.Git.IncludeUntrackedCount
}
func gitDetectUnpushed(c configFile) *bool {
	if c.Git == nil {
		return nil
	}
	return c.Git.DetectUnpushed
}
func gitCleanupMode(c configFile) *string {
	if c.Git == nil {
		return nil
	}
	return c.Git.CleanupMode
}
func boardDefaultFormat(c configFile) *string {
	if c.Board == nil {
		return nil
	}
	return c.Board.DefaultFormat
}
func boardHTMLSelfContained(c configFile) *bool {
	if c.Board == nil {
		return nil
	}
	return c.Board.HTMLSelfContained
}
func integrationsManagedMarker(c configFile) *string {
	if c.Integrations == nil {
		return nil
	}
	return c.Integrations.ManagedMarker
}
func integrationsInstructionTargets(c configFile) *[]string {
	if c.Integrations == nil {
		return nil
	}
	return c.Integrations.InstructionTargets
}

func validateConfig(c configFile) error {
	for _, value := range []*string{c.Workspace, projectNameOverride(c), projectDisplay(c), projectTaskPrefix(c), projectDefaultBranch(c), integrationsManagedMarker(c)} {
		if value != nil {
			if strings.TrimSpace(*value) == "" {
				return errors.New("configuration values must not be empty")
			}
			if err := rejectSecretLike(*value); err != nil {
				return err
			}
		}
	}
	if value := projectNameOverride(c); value != nil && !projectName.MatchString(*value) {
		return fmt.Errorf("invalid project name %q", *value)
	}
	if value := projectTaskPrefix(c); value != nil && !taskPrefix.MatchString(*value) {
		return fmt.Errorf("invalid task_prefix %q", *value)
	}
	if value := integrationsManagedMarker(c); value != nil && !managedMarker.MatchString(*value) {
		return fmt.Errorf("invalid managed_marker %q", *value)
	}
	if targets := integrationsInstructionTargets(c); targets != nil {
		if len(*targets) == 0 {
			return errors.New("instruction_targets must not be empty")
		}
		for _, target := range *targets {
			if strings.TrimSpace(target) == "" {
				return errors.New("instruction_targets must not contain empty values")
			}
		}
	}
	if value := coordinationMaxDepth(c); value != nil && (*value < 1 || *value > 32) {
		return errors.New("max_delegation_depth must be between 1 and 32")
	}
	if value := coordinationTaskClaimMode(c); value != nil && *value != string(TaskClaimModeExclusive) && *value != string(TaskClaimModeAdvisory) {
		return fmt.Errorf("invalid task_claim_mode %q", *value)
	}
	if value := coordinationDefaultDependencyUnblockOn(c); value != nil && *value != string(UnblockWhenWorkComplete) && *value != string(UnblockWhenVerifiedDone) {
		return fmt.Errorf("invalid default_dependency_unblock_on %q", *value)
	}
	if value := reservationMode(c); value != nil && *value != string(ReservationModeAdvisory) && *value != string(ReservationModeExclusive) {
		return fmt.Errorf("invalid reservation mode %q", *value)
	}
	if value := privacyPromptStorage(c); value != nil && *value != string(StorageModeRedacted) && *value != string(StorageModeFull) {
		return fmt.Errorf("invalid prompt_storage %q", *value)
	}
	if value := privacyFinalOutputStorage(c); value != nil && *value != string(StorageModeRedacted) && *value != string(StorageModeFull) {
		return fmt.Errorf("invalid final_output_storage %q", *value)
	}
	if value := privacyBoardShowPrompt(c); value != nil && *value != string(BoardShowPromptSummary) && *value != string(BoardShowPromptFull) {
		return fmt.Errorf("invalid board_show_prompt %q", *value)
	}
	if value := gitCleanupMode(c); value != nil && *value != string(CleanupModePlanOnly) && *value != string(CleanupModeAutomatic) {
		return fmt.Errorf("invalid cleanup_mode %q", *value)
	}
	if value := boardDefaultFormat(c); value != nil && *value != string(BoardFormatTTY) && *value != string(BoardFormatJSON) && *value != string(BoardFormatMarkdown) && *value != string(BoardFormatHTML) {
		return fmt.Errorf("invalid board default_format %q", *value)
	}
	for _, value := range []*string{coordinationSemanticStaleAfter(c), coordinationProcessStaleAfter(c), coordinationAgentDeadAfter(c), reservationDefaultTTL(c), gitScanInterval(c)} {
		if value != nil {
			if _, err := positiveDuration(*value); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateResolved(resolved Resolved) error {
	if resolved.Coordination.MaxDelegationDepth < 1 || resolved.Coordination.MaxDelegationDepth > 32 {
		return errors.New("max_delegation_depth must be between 1 and 32")
	}
	if resolved.Coordination.ProcessStaleAfter <= 0 || resolved.Coordination.SemanticStaleAfter <= 0 || resolved.Coordination.AgentDeadAfter <= 0 || resolved.Reservation.DefaultTTL <= 0 || resolved.Git.ScanInterval <= 0 {
		return errors.New("durations must be positive")
	}
	if resolved.Coordination.ProcessStaleAfter > resolved.Coordination.SemanticStaleAfter || resolved.Coordination.SemanticStaleAfter > resolved.Coordination.AgentDeadAfter {
		return errors.New("staleness durations must satisfy process <= semantic <= agent")
	}
	return validateConfig(configFile{Project: &ProjectOverrides{Name: optional(resolved.Project.Name), Display: optional(resolved.Project.Display), TaskPrefix: new(resolved.Project.TaskPrefix), DefaultBranch: new(resolved.Project.DefaultBranch)}, Coordination: &CoordinationOverrides{MaxDelegationDepth: new(resolved.Coordination.MaxDelegationDepth), TaskClaimMode: new(string(resolved.Coordination.TaskClaimMode)), DefaultDependencyUnblockOn: new(string(resolved.Coordination.DefaultDependencyUnblockOn))}, Reservation: &ReservationOverrides{Mode: new(string(resolved.Reservation.Mode))}, Privacy: &PrivacyOverrides{PromptStorage: new(string(resolved.Privacy.PromptStorage)), FinalOutputStorage: new(string(resolved.Privacy.FinalOutputStorage)), BoardShowPrompt: new(string(resolved.Privacy.BoardShowPrompt))}, Git: &GitOverrides{CleanupMode: new(string(resolved.Git.CleanupMode))}, Board: &BoardOverrides{DefaultFormat: new(string(resolved.Board.DefaultFormat))}, Integrations: &IntegrationOverrides{ManagedMarker: new(resolved.Integrations.ManagedMarker)}})
}

func optional(value string) *string {
	if value == "" {
		return nil
	}
	return new(value)
}
func positiveDuration(value string) (time.Duration, error) {
	duration, err := time.ParseDuration(value)
	if err != nil || duration <= 0 {
		return 0, fmt.Errorf("invalid positive duration %q", value)
	}
	return duration, nil
}
func rejectSecretLike(value string) error {
	if safety.IsDelegationToken(value) {
		return errors.New("configuration contains a delegation token")
	}
	if secretLike.MatchString(value) {
		return errors.New("configuration contains a secret-like value")
	}
	return nil
}
