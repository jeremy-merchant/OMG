package cli

func objectSchema(required []string, properties map[string]any) map[string]any {
	return map[string]any{"type": "object", "required": required, "additionalProperties": false, "properties": properties}
}

func stringProperty(description string) map[string]any {
	return map[string]any{"type": "string", "description": description}
}

func stringArrayProperty(description string) map[string]any {
	return map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": description}
}

// examplePayloadContract is the single copyable JSON payload catalog used by
// example discovery and CLI validation recovery. It contains only public field
// names and inert placeholder values.
func examplePayloadContract(topic string) (map[string]any, any, bool) {
	recipient := objectSchema(nil, map[string]any{
		"session_id": stringProperty("Exactly one recipient selector."),
		"human_id":   stringProperty("Exactly one recipient selector."),
		"task_id":    stringProperty("Exactly one recipient selector."),
		"role":       stringProperty("Exactly one recipient selector."),
	})
	contracts := map[string]struct {
		schema  map[string]any
		payload any
	}{
		"session-create": {
			objectSchema([]string{"id", "human_id", "runtime", "role", "native_access_state"}, map[string]any{
				"id": stringProperty("Stable worker session ID."), "human_id": stringProperty("Canonical human owner ID."),
				"runtime": stringProperty("Runtime name."), "role": stringProperty("Worker role."),
				"source_ref": stringProperty("Optional inert provenance reference."), "task_id": stringProperty("Optional linked task ID."),
				"worktree_ref": stringProperty("Optional selected project worktree."), "native_access_state": stringProperty("Usually unsupported for portable workers."),
			}),
			map[string]any{"id": "WORKER_SESSION_ID", "human_id": "HUMAN_ID", "runtime": "openai-codex", "role": "worker", "source_ref": "controller:CONTROLLER_SESSION_ID", "task_id": "TASK_ID", "native_access_state": "unsupported"},
		},
		"session-archive": {
			objectSchema([]string{"id", "session_id", "actor_session_id", "reason"}, map[string]any{
				"id": stringProperty("Unique archive event ID."), "session_id": stringProperty("Finished session to archive."),
				"actor_session_id": stringProperty("Controller session recording the archive."), "reason": stringProperty("Safe archive reason."),
			}),
			map[string]any{"id": "ARCHIVE_EVENT_ID", "session_id": "FINISHED_SESSION_ID", "actor_session_id": "CONTROLLER_SESSION_ID", "reason": "all owned runs are terminal"},
		},
		"message-inbox": {
			objectSchema([]string{"recipient"}, map[string]any{"recipient": recipient}),
			map[string]any{"recipient": map[string]any{"session_id": "WORKER_SESSION_ID"}},
		},
		"progress-add": {
			objectSchema([]string{"id", "task_id", "session_id", "phase", "done", "doing", "next"}, map[string]any{
				"id": stringProperty("Unique progress event ID."), "task_id": stringProperty("Task ID."), "run_id": stringProperty("Optional run ID."),
				"session_id": stringProperty("Reporting session ID."), "phase": stringProperty("inspect, plan, implement, test, review, or wait."),
				"done": stringArrayProperty("Completed facts."), "doing": stringArrayProperty("Current work."), "next": stringArrayProperty("Next actions."),
				"supersedes_id": stringProperty("Optional prior progress event ID."),
			}),
			map[string]any{"id": "PROGRESS_ID", "task_id": "TASK_ID", "run_id": "RUN_ID", "session_id": "WORKER_SESSION_ID", "phase": "implement", "done": []string{"inspected task"}, "doing": []string{"implementing change"}, "next": []string{"run tests"}},
		},
		"handoff-create": {
			objectSchema([]string{"id", "task_id", "run_id", "source_session_id", "summary", "final_output_policy"}, map[string]any{
				"id": stringProperty("Unique handoff ID."), "task_id": stringProperty("Task ID."), "run_id": stringProperty("Run ID."),
				"source_session_id": stringProperty("Submitting worker session."), "target_session_id": stringProperty("Optional reviewer session."),
				"summary": stringProperty("Safe summary."), "final_output_policy": stringProperty("none, hash_only, redacted, or full."),
				"final_output_hash": stringProperty("Required by hash_only."), "source_commit": stringProperty("Exact source commit."), "source_tree": stringProperty("Exact source tree."),
				"changed_files": stringArrayProperty("Project-relative changed files."), "commits": stringArrayProperty("Source commits."),
				"verification_evidence": map[string]any{"type": "array", "items": objectSchema([]string{"summary", "hash"}, map[string]any{"summary": stringProperty("Safe verification summary."), "hash": stringProperty("Evidence hash.")})},
				"remaining_risks":       stringArrayProperty("Known residual risks."), "suggested_actions": stringArrayProperty("Reviewer next actions."),
			}),
			map[string]any{"id": "HANDOFF_ID", "task_id": "TASK_ID", "run_id": "RUN_ID", "source_session_id": "WORKER_SESSION_ID", "target_session_id": "CONTROLLER_SESSION_ID", "summary": "Implementation and verification complete.", "final_output_policy": "hash_only", "final_output_hash": "sha256:OUTPUT_HASH", "source_commit": "SOURCE_SHA", "source_tree": "SOURCE_TREE", "changed_files": []string{"path/to/file.go"}, "commits": []string{"SOURCE_SHA"}, "verification_evidence": []any{map[string]any{"summary": "targeted tests passed", "hash": "sha256:EVIDENCE_HASH"}}, "remaining_risks": []string{}, "suggested_actions": []string{"review exact SHA"}},
		},
		"handoff-accept": {
			objectSchema([]string{"handoff_id", "actor_session_id"}, map[string]any{"handoff_id": stringProperty("Submitted handoff ID."), "decision_id": stringProperty("Optional stable decision ID."), "actor_session_id": stringProperty("Reviewer/controller session ID.")}),
			map[string]any{"handoff_id": "HANDOFF_ID", "actor_session_id": "CONTROLLER_SESSION_ID"},
		},
		"checkpoint-record": {
			objectSchema([]string{"id", "session_id", "liveness"}, map[string]any{"id": stringProperty("Unique checkpoint ID."), "session_id": stringProperty("Worker session ID."), "liveness": stringProperty("alive, stale, or interrupted."), "detail": stringProperty("Optional safe detail.")}),
			map[string]any{"id": "CHECKPOINT_ID", "session_id": "WORKER_SESSION_ID", "liveness": "alive", "detail": "working"},
		},
		"reserve-add": {
			objectSchema([]string{"id", "pattern_kind", "pattern", "case_sensitivity", "mode", "human_id", "session_id", "task_id", "run_id", "intent", "ttl_seconds"}, map[string]any{
				"id": stringProperty("Unique reservation ID."), "pattern_kind": stringProperty("exact or glob."), "pattern": stringProperty("Project-relative path pattern."),
				"case_sensitivity": stringProperty("sensitive or insensitive."), "mode": stringProperty("exclusive or shared."), "human_id": stringProperty("Canonical human ID."),
				"session_id": stringProperty("Worker session ID."), "task_id": stringProperty("Task ID."), "run_id": stringProperty("Run ID."), "intent": stringProperty("Bounded edit intent."),
				"ttl_seconds": map[string]any{"type": "integer", "minimum": 1, "description": "Reservation lifetime."},
			}),
			map[string]any{"id": "RESERVATION_ID", "pattern_kind": "glob", "pattern": "internal/example/**", "case_sensitivity": "sensitive", "mode": "exclusive", "human_id": "HUMAN_ID", "session_id": "WORKER_SESSION_ID", "task_id": "TASK_ID", "run_id": "RUN_ID", "intent": "edit worker bootstrap", "ttl_seconds": 3600},
		},
	}
	contract, ok := contracts[topic]
	return contract.schema, contract.payload, ok
}
