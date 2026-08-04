// Package app contains transport-neutral command and query application contracts.
package app

import (
	"context"

	"github.com/jeremy-merchant/oh-my-group/internal/app/query"
	"github.com/jeremy-merchant/oh-my-group/internal/domain"
)

// Command is a typed application request. Implementations define stable names
// and validation without carrying transport-specific input.
type Command interface {
	CommandName() string
}

// Query is a typed read-only application request.
type Query interface {
	QueryName() string
}

// CommandHandler handles a command for a resolved actor.
type CommandHandler interface {
	Handle(context.Context, domain.ActorContext, Command) (domain.Result, error)
}

// QueryHandler constructs one canonical, redacted application snapshot.
type QueryHandler interface {
	Query(context.Context, domain.ActorContext, Query) (query.ViewModel, error)
}
