package chitta

import (
	"context"
	"errors"
	"time"

	"zbot/backend/internal/domain"
)

// emit publishes one ChittaToolInvoked event per tool invocation —
// success or failure. Publication is best-effort: a missing bus or a
// failed publish never fails the tool call (contracts "Every tool call
// is observable").
func (s *System) emit(ctx context.Context, tool, outcome, errCode string, affected []string, host domain.ChittaHostContext) {
	if s.bus == nil {
		return
	}
	prov := host.Provenance()
	ev := domain.TimelineEvent{
		ID:        newID("evt"),
		SessionID: host.SessionID,
		Agent:     host.AgentID,
		Kind:      domain.EventChittaToolInvoked,
		Timestamp: time.Now().UTC(),
		Text:      tool + " " + outcome,
		ChittaToolInvoked: &domain.ChittaToolInvokedPayload{
			Tool:        tool,
			Outcome:     outcome,
			ErrorCode:   errCode,
			AffectedIDs: affected,
			Provenance:  &prov,
		},
	}
	_ = s.bus.Publish(ctx, ev) // best-effort by contract
}

// emitSeam runs op and — ONLY for trusted-host (distiller) writes —
// emits exactly one ChittaToolInvoked event with the op's outcome. The
// agent/user path emits in Dispatch instead; exactly one event per
// logical operation either way (spec chitta-distillation "Distiller
// observability").
func emitSeam[T any](s *System, ctx context.Context, tool string, host domain.ChittaHostContext, op func() (T, []string, error)) (T, error) {
	out, affected, err := op()
	if host.Source == domain.SourceDistiller {
		outcome := "success"
		code := ""
		if err != nil {
			outcome = "error"
			var ce *domain.ChittaError
			if errors.As(err, &ce) {
				code = string(ce.Code)
			}
		}
		if affected == nil {
			affected = []string{}
		}
		s.emit(ctx, tool, outcome, code, affected, host)
	}
	return out, err
}
