# Worker bootstrap and atomic runtime launch

This workflow lets a controller complete OMG registration before a cmux/OMP lane begins code work. It keeps the worker on `board me`, makes the five identity values explicit, and avoids placing long prompts or placeholder text in a shell command.

## Controller preparation

The controller creates the human, task, and a task-bound worker session through the normal strict payload commands. A delegated session should be registered through `delegate issue`/`delegate register`; a human-direct session can be created with `session create`. The session must carry the same `human_id` and `task_id` that bootstrap will receive.

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
