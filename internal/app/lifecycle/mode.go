// Package lifecycle classifies repository work into proportional OMG lifecycles.
package lifecycle

import (
	"fmt"
	"strings"
)

// Mode is the amount of coordination state a unit of work requires.
type Mode string

const (
	Observe  Mode = "OBSERVE"
	WorkLite Mode = "WORK_LITE"
	Full     Mode = "FULL"
)

// VerificationLevel describes the minimum proportional verification policy.
type VerificationLevel string

const (
	VerificationNone   VerificationLevel = "NONE"
	VerificationLow    VerificationLevel = "LOW"
	VerificationMedium VerificationLevel = "MEDIUM"
	VerificationHigh   VerificationLevel = "HIGH"
)

// Input contains only stable risk signals. It deliberately does not inspect a
// repository or mutate coordination state.
type Input struct {
	MutatesFiles               bool   `json:"mutates_files"`
	CreatesBranch              bool   `json:"creates_branch"`
	CreatesWorktree            bool   `json:"creates_worktree"`
	UsesMultipleAgents         bool   `json:"uses_multiple_agents"`
	TouchesProduction          bool   `json:"touches_production"`
	TouchesDatabase            bool   `json:"touches_database"`
	TouchesAuthOrPayment       bool   `json:"touches_auth_or_payment"`
	ChangesUserVisibleBehavior bool   `json:"changes_user_visible_behavior"`
	ExternalSideEffects        bool   `json:"external_side_effects"`
	ReleaseOrCanary            bool   `json:"release_or_canary"`
	RequiresHandoff            bool   `json:"requires_handoff"`
	ExpectedDurationMinutes    int    `json:"expected_duration_minutes,omitempty"`
	Override                   string `json:"override,omitempty"`
}

// Contract is the complete policy decision returned to an agent or adapter.
type Contract struct {
	Mode                             Mode              `json:"mode"`
	LedgerRole                       string            `json:"ledger_role"`
	PreflightRequired                bool              `json:"preflight_required"`
	StartRegistrationRequired        bool              `json:"start_registration_required"`
	FinishRegistrationRequired       bool              `json:"finish_registration_required"`
	IntermediateLedgerWritesRequired bool              `json:"intermediate_ledger_writes_required"`
	ControllerProvidesCommands       bool              `json:"controller_provides_commands"`
	CommandDiscoveryAllowed          bool              `json:"command_discovery_allowed"`
	NonSafetyErrorsAreWarnings       bool              `json:"non_safety_errors_are_warnings"`
	FailClosedOnPreflightFailure     bool              `json:"fail_closed_on_preflight_failure"`
	FailClosedOnOwnershipConflict    bool              `json:"fail_closed_on_ownership_conflict"`
	SessionRequired                  bool              `json:"session_required"`
	TaskRequired                     bool              `json:"task_required"`
	RunRequired                      bool              `json:"run_required"`
	ProgressRequired                 bool              `json:"progress_required"`
	ReservationRequired              bool              `json:"reservation_required"`
	HandoffRequired                  bool              `json:"handoff_required"`
	IndependentVerificationRequired  bool              `json:"independent_verification_required"`
	AutoArchive                      bool              `json:"auto_archive"`
	VerificationLevel                VerificationLevel `json:"verification_level"`
	SourceOfTruth                    []string          `json:"source_of_truth"`
	Reasons                          []string          `json:"reasons"`
}

// Classify applies conservative, deterministic risk rules. Explicit overrides
// may raise the lifecycle level, but cannot downgrade intrinsically FULL work.
func Classify(input Input) (Contract, error) {
	if input.ExpectedDurationMinutes < 0 {
		return Contract{}, fmt.Errorf("expected_duration_minutes must be nonnegative")
	}
	override, err := parseOverride(input.Override)
	if err != nil {
		return Contract{}, err
	}

	mode := WorkLite
	reasons := make([]string, 0, 6)
	fullRisk := input.UsesMultipleAgents || input.TouchesProduction || input.TouchesDatabase || input.TouchesAuthOrPayment || input.ReleaseOrCanary || input.RequiresHandoff
	readOnly := !input.MutatesFiles && !input.CreatesBranch && !input.CreatesWorktree && !input.ChangesUserVisibleBehavior && !input.ExternalSideEffects && !fullRisk
	switch {
	case fullRisk:
		mode = Full
		reasons = append(reasons, fullReasons(input)...)
	case override != "":
		mode = override
		reasons = append(reasons, "explicit_override")
	case readOnly:
		mode = Observe
		reasons = append(reasons, "read_only_no_external_side_effect")
	default:
		mode = WorkLite
		reasons = append(reasons, "single_owner_repository_change")
	}

	// A downgrade override never suppresses a safety-triggered FULL lifecycle.
	if fullRisk && override != "" && override != Full {
		reasons = append(reasons, "unsafe_downgrade_ignored")
	}

	contract := Contract{
		Mode:                       mode,
		Reasons:                    reasons,
		CommandDiscoveryAllowed:    false,
		NonSafetyErrorsAreWarnings: true,
		SourceOfTruth:              []string{"git_sha", "clean_tree", "diff", "reachability"},
	}
	switch mode {
	case Observe:
		contract.LedgerRole = "none"
		contract.AutoArchive = true
		contract.VerificationLevel = VerificationNone
	case WorkLite:
		contract.LedgerRole = "integration_boundary"
		contract.PreflightRequired = true
		contract.StartRegistrationRequired = true
		contract.FinishRegistrationRequired = true
		contract.ControllerProvidesCommands = true
		contract.FailClosedOnPreflightFailure = true
		contract.FailClosedOnOwnershipConflict = true
		contract.SessionRequired = true
		contract.TaskRequired = true
		contract.RunRequired = true
		contract.ProgressRequired = input.ExpectedDurationMinutes >= 15
		contract.ReservationRequired = input.MutatesFiles
		contract.AutoArchive = true
		contract.VerificationLevel = VerificationLow
		if input.ChangesUserVisibleBehavior {
			contract.VerificationLevel = VerificationMedium
		}
	case Full:
		contract.LedgerRole = "full_coordination"
		contract.PreflightRequired = true
		contract.StartRegistrationRequired = true
		contract.FinishRegistrationRequired = true
		contract.IntermediateLedgerWritesRequired = true
		contract.ControllerProvidesCommands = true
		contract.FailClosedOnPreflightFailure = true
		contract.FailClosedOnOwnershipConflict = true
		contract.SessionRequired = true
		contract.TaskRequired = true
		contract.RunRequired = true
		contract.ProgressRequired = true
		contract.ReservationRequired = input.MutatesFiles || input.CreatesBranch || input.CreatesWorktree
		contract.HandoffRequired = true
		contract.IndependentVerificationRequired = true
		contract.VerificationLevel = VerificationMedium
		if input.TouchesProduction || input.TouchesAuthOrPayment || input.ReleaseOrCanary {
			contract.VerificationLevel = VerificationHigh
		}
	}
	return contract, nil
}

// ContractFor returns the baseline policy for a user-selected mode.
func ContractFor(mode Mode) (Contract, error) {
	return Classify(Input{Override: string(mode)})
}

func parseOverride(value string) (Mode, error) {
	value = strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(value), "-", "_"))
	switch Mode(value) {
	case "":
		return "", nil
	case Observe, WorkLite, Full:
		return Mode(value), nil
	default:
		return "", fmt.Errorf("override must be OBSERVE, WORK_LITE, or FULL")
	}
}

func fullReasons(input Input) []string {
	reasons := make([]string, 0, 6)
	if input.UsesMultipleAgents {
		reasons = append(reasons, "multiple_agents")
	}
	if input.TouchesProduction {
		reasons = append(reasons, "production")
	}
	if input.TouchesDatabase {
		reasons = append(reasons, "database")
	}
	if input.TouchesAuthOrPayment {
		reasons = append(reasons, "auth_or_payment")
	}
	if input.ReleaseOrCanary {
		reasons = append(reasons, "release_or_canary")
	}
	if input.RequiresHandoff {
		reasons = append(reasons, "ownership_transfer")
	}
	return reasons
}
