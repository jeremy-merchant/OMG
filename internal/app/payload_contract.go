package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"sort"
	"strings"
	"unicode"

	"github.com/jeremy-merchant/oh-my-group/internal/app/query"
	"github.com/jeremy-merchant/oh-my-group/internal/domain"
)

type publicPayloadContract struct {
	prototype func() any
	required  map[string]string
	aliases   map[string]string
	nested    map[string]publicPayloadContract
	oneOf     []string
}

func validatePublicPayload(command string, payload []byte) domain.DomainError {
	contract, ok := publicPayloadContracts()[command]
	if !ok {
		return domain.DomainError{}
	}
	return validatePayloadObject(payload, contract, "")
}

func publicPayloadContracts() map[string]publicPayloadContract {
	recipient := publicPayloadContract{
		prototype: func() any { return &coordinationRecipientPayload{} },
		oneOf:     []string{"session_id", "human_id", "task_id", "role"},
		aliases: map[string]string{
			"recipient_session_id":  "session_id",
			"recipient_session_ids": "session_id",
			"recipient_id":          "session_id",
		},
	}
	return map[string]publicPayloadContract{
		"board.query": {
			prototype: func() any { return &query.BoardRequest{} },
			required:  map[string]string{"mode": "string"},
		},
		"session.create": {
			prototype: func() any { return &lineageSessionPayload{} },
			required:  map[string]string{"human_id": "string", "runtime": "string", "role": "string", "native_access_state": "string"},
		},
		"session.archive": {
			prototype: func() any { return &lineageSessionArchivePayload{} },
			required:  map[string]string{"id": "string", "session_id": "string", "actor_session_id": "string", "reason": "string"},
		},
		"task.create": {
			prototype: func() any { return &TaskCreate{} },
			required:  map[string]string{"title": "string", "created_by_session_id": "string"},
		},
		"task.get": {
			prototype: func() any { return &lineageTaskGetPayload{} },
			required:  map[string]string{"task_id": "string"},
		},
		"task.claim": {
			prototype: func() any { return &lineageTaskClaimPayload{} },
			required:  map[string]string{"task_id": "string", "session_id": "string"},
		},
		"task.transition": {
			prototype: func() any { return &lineageTaskTransitionPayload{} },
			required:  map[string]string{"task_id": "string", "state": "string"},
		},
		"task.run-create": {
			prototype: func() any { return &lineageRunCreatePayload{} },
			required:  map[string]string{"task_id": "string", "session_id": "string"},
		},
		"task.run-transition": {
			prototype: func() any { return &lineageRunTransitionPayload{} },
			required:  map[string]string{"run_id": "string", "state": "string"},
		},
		"task.finish-lite": {
			prototype: func() any { return &lineageFinishLitePayload{} },
			required:  map[string]string{"task_id": "string", "run_id": "string", "session_id": "string", "actor_session_id": "string", "archive_event_id": "string", "evidence": "string"},
		},
		"candidate.close": {
			prototype: func() any { return &candidateClosePayload{} },
			required:  map[string]string{"handoff_id": "string", "actor_session_id": "string", "archive_event_id": "string", "evidence": "string"},
		},
		"message.inbox": {
			prototype: func() any { return &coordinationInboxPayload{} },
			required:  map[string]string{"recipient": "object"},
			aliases: map[string]string{
				"recipient_session_id": "recipient.session_id", "recipient_session_ids": "recipient.session_id",
				"recipient_id": "recipient", "session_id": "recipient.session_id",
			},
			nested: map[string]publicPayloadContract{"recipient": recipient},
		},
		"progress.add": {
			prototype: func() any { return &coordinationProgressPayload{} },
			required:  map[string]string{"id": "string", "task_id": "string", "session_id": "string", "phase": "string", "done": "array", "doing": "array", "next": "array"},
		},
		"handoff.create": {
			prototype: func() any { return &HandoffCreate{} },
			required:  map[string]string{"id": "string", "task_id": "string", "run_id": "string", "source_session_id": "string", "summary": "string", "final_output_policy": "string"},
		},
		"handoff.accept": {
			prototype: func() any { return &coordinationDecisionPayload{} },
			required:  map[string]string{"handoff_id": "string", "actor_session_id": "string"},
		},
		"checkpoint.record": {
			prototype: func() any { return &lineageCheckpointPayload{} },
			required:  map[string]string{"id": "string", "session_id": "string", "liveness": "string"},
		},
		"reserve.add": {
			prototype: func() any { return &reserveAddPayload{} },
			required:  map[string]string{"id": "string", "pattern_kind": "string", "pattern": "string", "case_sensitivity": "string", "mode": "string", "human_id": "string", "session_id": "string", "task_id": "string", "run_id": "string", "intent": "string", "ttl_seconds": "integer"},
		},
		"reserve.batch-add": {
			prototype: func() any { return &reserveBatchAddPayload{} },
			required:  map[string]string{"human_id": "string", "session_id": "string", "task_id": "string", "run_id": "string", "items": "array"},
		},
		"worker.setup": {
			prototype: func() any { return &workerSetupPayload{} },
			required: map[string]string{
				"human_id": "string", "controller_session_id": "string", "session_id": "string",
				"runtime": "string", "role": "string", "task_id": "string", "task_title": "string",
				"run_id": "string", "reservations": "array",
			},
		},
	}
}

func validatePayloadObject(payload []byte, contract publicPayloadContract, prefix string) domain.DomainError {
	var fields map[string]json.RawMessage
	decoder := json.NewDecoder(bytes.NewReader(payload))
	if err := decoder.Decode(&fields); err != nil || decoder.Decode(&struct{}{}) != io.EOF || fields == nil {
		return domain.NewError(domain.CodeInvalidArgument, "payload must be one JSON object", false)
	}
	allowed := jsonFieldNames(contract.prototype())
	for field := range fields {
		if _, ok := allowed[field]; ok {
			continue
		}
		path := safeFieldPath(prefix, field)
		expected := contract.aliases[field]
		if expected == "" {
			expected = nearestField(field, allowed)
		}
		if expected != "" {
			expected = joinFieldPath(prefix, expected)
			return domain.NewError(domain.CodeInvalidArgument, fmt.Sprintf("unknown field %s; expected %s", path, expected), false)
		}
		return domain.NewError(domain.CodeInvalidArgument, fmt.Sprintf("unknown field %s", path), false)
	}
	for field, kind := range contract.required {
		raw, ok := fields[field]
		if !ok || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			return domain.NewError(domain.CodeInvalidArgument, fmt.Sprintf("missing required field %s; expected %s", joinFieldPath(prefix, field), kind), false)
		}
		if kind == "string" {
			var value string
			if json.Unmarshal(raw, &value) == nil && strings.TrimSpace(value) == "" {
				return domain.NewError(domain.CodeInvalidArgument, fmt.Sprintf("field %s must be a non-empty string", joinFieldPath(prefix, field)), false)
			}
		}
	}
	for field, nested := range contract.nested {
		if raw, ok := fields[field]; ok {
			if err := validatePayloadObject(raw, nested, joinFieldPath(prefix, field)); err.Code != "" {
				return err
			}
		}
	}
	target := contract.prototype()
	strict := json.NewDecoder(bytes.NewReader(payload))
	strict.DisallowUnknownFields()
	if err := strict.Decode(target); err != nil {
		var typeErr *json.UnmarshalTypeError
		if errors.As(err, &typeErr) {
			field := joinFieldPath(prefix, typeErr.Field)
			return domain.NewError(domain.CodeInvalidArgument, fmt.Sprintf("field %s has type %s; expected %s", safeFieldPath("", field), typeErr.Value, typeErr.Type.String()), false)
		}
		return domain.NewError(domain.CodeInvalidArgument, "payload contains an invalid field value", false)
	}
	if len(contract.oneOf) != 0 {
		selected := 0
		for _, field := range contract.oneOf {
			var value string
			if raw, ok := fields[field]; ok && json.Unmarshal(raw, &value) == nil && strings.TrimSpace(value) != "" {
				selected++
			}
		}
		if selected != 1 {
			paths := make([]string, len(contract.oneOf))
			for i, field := range contract.oneOf {
				paths[i] = joinFieldPath(prefix, field)
			}
			return domain.NewError(domain.CodeInvalidArgument, "exactly one recipient selector is required: "+strings.Join(paths, ", "), false)
		}
	}
	return domain.DomainError{}
}

func jsonFieldNames(value any) map[string]struct{} {
	t := reflect.TypeOf(value)
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	result := make(map[string]struct{}, t.NumField())
	for i := 0; i < t.NumField(); i++ {
		name := strings.Split(t.Field(i).Tag.Get("json"), ",")[0]
		if name != "" && name != "-" {
			result[name] = struct{}{}
		}
	}
	return result
}

func safeFieldPath(prefix, field string) string {
	path := joinFieldPath(prefix, field)
	if len(path) > 128 || domain.ContainsSensitiveStableMetadata(path) {
		return "payload field"
	}
	for _, r := range path {
		if !(unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '-' || r == '.') {
			return "payload field"
		}
	}
	return path
}

func joinFieldPath(prefix, field string) string {
	if prefix == "" {
		return field
	}
	if strings.HasPrefix(field, prefix+".") {
		return field
	}
	return prefix + "." + field
}

func nearestField(field string, allowed map[string]struct{}) string {
	names := make([]string, 0, len(allowed))
	for name := range allowed {
		names = append(names, name)
	}
	sort.Strings(names)
	best, distance := "", 4
	for _, name := range names {
		if candidate := fieldDistance(field, name); candidate < distance {
			best, distance = name, candidate
		}
	}
	return best
}

func fieldDistance(first, second string) int {
	a, b := []rune(first), []rune(second)
	previous := make([]int, len(b)+1)
	for i := range previous {
		previous[i] = i
	}
	for i, left := range a {
		current := make([]int, len(b)+1)
		current[0] = i + 1
		for j, right := range b {
			cost := 0
			if left != right {
				cost = 1
			}
			current[j+1] = minPayloadInt(current[j]+1, previous[j+1]+1, previous[j]+cost)
		}
		previous = current
	}
	return previous[len(b)]
}

func minPayloadInt(values ...int) int {
	result := values[0]
	for _, value := range values[1:] {
		if value < result {
			result = value
		}
	}
	return result
}
