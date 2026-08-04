package lineage

import (
	"context"
	"time"

	"github.com/jeremy-merchant/oh-my-group/internal/domain"
	core "github.com/jeremy-merchant/oh-my-group/internal/domain/lineage"
	"github.com/jeremy-merchant/oh-my-group/internal/ports"
)

// CreateSessionInTransaction applies the same canonical session prerequisites as
// session.create while remaining inside an existing Store.Write callback.
func CreateSessionInTransaction(ctx context.Context, repositories ports.Repositories, session core.AgentSession) error {
	if session.HumanID != "" {
		_, found, err := repositories.Coordination().GetHuman(ctx, session.HumanID)
		if err != nil {
			return err
		}
		if !found {
			return humanNotFound()
		}
	}
	return repositories.Coordination().CreateSession(ctx, session)
}

// CreateTaskInTransaction validates hierarchy policy and creates one task inside
// an existing Store.Write callback.
func CreateTaskInTransaction(ctx context.Context, repositories ports.Repositories, task core.Task) (core.Task, error) {
	if err := validateTaskParent(ctx, repositories.Coordination(), task); err != nil {
		return core.Task{}, err
	}
	return repositories.Coordination().CreateTask(ctx, task)
}

// ClaimTaskInTransaction preserves the canonical single-winner claim contract
// inside a compound transaction.
func ClaimTaskInTransaction(ctx context.Context, repositories ports.Repositories, now time.Time, taskID, sessionID core.ID) (core.Task, bool, error) {
	return repositories.Coordination().ClaimTask(ctx, taskID, sessionID, now)
}

// CreateRunInTransaction applies the same task/session liveness prerequisites as
// task.run-create while remaining inside an existing Store.Write callback.
func CreateRunInTransaction(ctx context.Context, repositories ports.Repositories, run core.TaskRun) error {
	task, found, err := repositories.Coordination().GetTask(ctx, run.TaskID)
	if err != nil {
		return err
	}
	if !found {
		return notFound()
	}
	if terminalTask(task.State) {
		return conflict()
	}
	session, found, err := repositories.Coordination().GetSession(ctx, run.SessionID)
	if err != nil {
		return err
	}
	if !found {
		return notFound()
	}
	if terminalSession(session) {
		return conflict()
	}
	if task.ProjectID != session.ProjectID || task.ClaimedBySessionID != "" && task.ClaimedBySessionID != session.ID {
		return domain.NewError(domain.CodeConflict, "run lineage does not match task ownership", false)
	}
	return repositories.Coordination().CreateRun(ctx, run)
}
