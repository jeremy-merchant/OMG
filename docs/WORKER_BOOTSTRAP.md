# Worker bootstrap and atomic runtime launch

This workflow lets a controller complete OMG registration before a cmux/OMP lane begins code work. It keeps the worker on `board me`, makes the five identity values explicit, and avoids placing long prompts or placeholder text in a shell command.

## Controller preparation

The canonical owner and a live controller session must exist first. When the worker session, Task, active run, and initial reservations do not yet exist, run `omg example show worker-setup --json`, save that strict payload, and call `omg worker setup --project /absolute/project --idempotency-key worker-setup-1 --payload-file worker-setup.json --json`. This performs one canonical transaction: it creates or validates the worker session, creates or validates and claims the Task, creates or validates the active run, and ensures the initial reservations. Any hierarchy, controller, ownership, run, reservation, or storage conflict rolls back the whole execution unit. Identical idempotency replay creates no duplicates; changed normalized intent under the same key is rejected. A delegated session should still be registered through `delegate issue`/`delegate register` when exact delegated lineage is required.

Create a private directory and environment destination, then run bootstrap before launching the runtime:

```bash
umask 077
bootstrap_dir="$(mktemp -d)"
bootstrap_env="$bootstrap_dir/worker.env"

OMG_PROJECT=/absolute/project \
OMG_SESSION_ID=worker-1 \
OMG_TASK_ID=TASK_ID \
OMG_CONTROLLER_SESSION_ID=controller-1 \
OMG_HUMAN_ID=HUMAN_ID \
omg worker bootstrap \
  --idempotency-key bootstrap-worker-1 \
  --output "$bootstrap_env" \
  --json
```

The JSON result includes health, whether a session was created or task claimed, the inbox, a worker-scoped board, the generated environment, and `next_action.argv`. Bootstrap fails closed when migrations are pending or when an existing session belongs to another human, task, or controller.

## Atomic cmux/OMP launch

Write the prompt to an owner-only file. Pass only a short launcher path or fixed argv through cmux; do not inject the prompt text with repeated `send` operations. Here `reviewed-prompt.txt` already contains the controller-reviewed prompt:

```bash
prompt_file="$bootstrap_dir/prompt.txt"
install -m 600 /absolute/path/to/reviewed-prompt.txt "$prompt_file"

(
  set -a
  . "$bootstrap_env"
  set +a
  exec omp <"$prompt_file"
)
```

If the selected OMP runtime exposes a prompt-file option, pass `"$prompt_file"` as one argv value instead of stdin. The important contract is atomic argv/stdin delivery: never interpolate prompt text, IDs, or placeholders such as `<handoff_id>` into a shell command. Message bodies and model output remain inert data.

Inside the worker lane, the normal read path is:

```bash
omg board me --json
omg message inbox --payload '{"recipient":{"session_id":"'"$OMG_SESSION_ID"'"}}' --json
```

The controller may use `omg board all` and `omg integration queue`; workers should not need either command to begin their assigned task.

## Reserve the initial edit scope in one call

After `omg task run-create` returns the canonical run ID, place that value in `run_id`, collect the currently known project-relative paths, and reserve them with one atomic request. Do not issue one `reserve add` command per file.

```bash
cat >"$bootstrap_dir/reservations.json" <<'JSON'
{
  "human_id": "HUMAN_ID",
  "session_id": "WORKER_SESSION_ID",
  "task_id": "TASK_ID",
  "run_id": "RUN_ID",
  "items": [
    {
      "id": "reservation-app",
      "pattern_kind": "exact",
      "pattern": "internal/app/service.go",
      "case_sensitivity": "sensitive",
      "mode": "exclusive",
      "intent": "edit application logic",
      "ttl_seconds": 3600
    },
    {
      "id": "reservation-test",
      "pattern_kind": "exact",
      "pattern": "internal/app/service_test.go",
      "case_sensitivity": "sensitive",
      "mode": "exclusive",
      "intent": "add regression coverage",
      "ttl_seconds": 3600
    }
  ]
}
JSON

omg reserve batch-add --project "$OMG_PROJECT" \
  --idempotency-key "reserve-batch-$OMG_TASK_ID-$OMG_SESSION_ID" \
  --payload-file "$bootstrap_dir/reservations.json" \
  --json
```

The operation validates the entire bounded batch and checks every conflict before the first insert. A strict conflict or storage failure commits none of the items. Use `reserve add` only when the scope is genuinely one path. If inspection later reveals more paths, collect the newly discovered paths and submit one additional batch rather than calling once per file.
