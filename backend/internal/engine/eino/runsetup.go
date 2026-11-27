// The run prologue shared by the legacy loop and the ADK path
// (extracted from adapter.go, LOC budget): attribution, the emit
// closure, the cached chat model and the goal row.

package eino

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/cloudwego/eino/schema"

	"zbot/backend/internal/domain"
	"zbot/backend/internal/store"
)

// emitFunc is the run's durable-first emit (attempted append → publish
// → stream) with the run's attribution and child surface rule baked in.
type emitFunc func(kind domain.TimelineEventKind, text string)

// runSetup is what a run's prologue produces.
type runSetup struct {
	in        runInput
	events    chan domain.TimelineEvent
	chatModel chatGenerator
	emit      emitFunc
}

// prepareRun runs the prologue every run (root or child) shares. A
// failure is DESCRIBED on the stream and finishes the execution before
// it returns (the caller only forwards the error).
func (a *Adapter) prepareRun(ctx context.Context, events chan domain.TimelineEvent, in runInput) (*runSetup, error) {
	// Attribution for this run: host-level events (ward transitions)
	// and — for the root — the execution row's agent column (rows are
	// created before the run knows its agent).
	a.setRunAttribution(in.sessionID, in)
	if !in.isChild {
		_ = a.st.SetExecutionAgent(in.execID, in.agent()) //nolint:errcheck // provenance only; attribution still rides the events
	}
	s := &runSetup{in: in, events: events}
	s.emit = func(kind domain.TimelineEventKind, text string) {
		chanKind, agent := kind, in.agent()
		if in.isChild {
			// Child steps surface in the parent's stream as delegation
			// events (the durable rows keep the original kinds under
			// the child's execution, attributed by agent).
			chanKind = domain.EventDelegation
		}
		evt := domain.TimelineEvent{
			ID: a.idGen(), SessionID: in.sessionID, ExecutionID: in.execID, Kind: chanKind,
			Text: text, Agent: agent,
		}
		// ATTEMPTED-durable-first (F-15, pinned by characterization tests):
		// the append error is IGNORED — publish and stream happen anyway, so
		// clients may observe events that failed to persist. The chat site
		// does the OPPOSITE (skips publish on append failure). The
		// retry/log/publish-anyway policy is owned by follow-up
		// backend-engine-run-ownership.
		payload, _ := json.Marshal(map[string]any{"text": text, "agent": agent})
		_ = a.st.AppendEvent(store.Event{
			ID: evt.ID, SessionID: in.sessionID, ExecutionID: in.execID,
			Kind: string(kind), Payload: string(payload),
		})
		if a.bus != nil {
			_ = a.bus.Publish(context.Background(), evt) // ctx-ok: F-15 fire-and-forget emit
		}
		select {
		case events <- evt:
		case <-ctx.Done():
		}
	}

	// Build or reuse the chat model (cached per provider+model+key to
	// avoid re-initializing the HTTP client on every run).
	modelKey := in.providerName + "/" + in.modelID + "/" + hashToken(in.token)
	a.mu.Lock()
	chatModel, ok := a.models[modelKey]
	a.mu.Unlock()
	if !ok {
		var err error
		chatModel, err = a.newModel(ctx, in.prov, in.token, in.modelID)
		if err != nil {
			s.emit(domain.EventAgentStep, "engine: failed to create chat model: "+err.Error())
			_ = a.st.FinishExecution(in.execID, "failed", err.Error(), 0, 0)
			return nil, fmt.Errorf("engine: chat model: %w", err)
		}
		a.mu.Lock()
		a.models[modelKey] = chatModel
		a.mu.Unlock()
	}
	s.chatModel = chatModel

	// Append the goal as the opening user message.
	_, err := a.st.Append(store.Message{
		ID: fmt.Sprintf("msg-goal-%d-%d", time.Now().UnixNano(), nextID()), SessionID: in.sessionID,
		ExecutionID: in.execID, Role: "user", Agent: in.agent(), Content: in.goal,
	})
	if err != nil {
		s.emit(domain.EventAgentStep, "engine: failed to persist goal: "+err.Error())
		_ = a.st.FinishExecution(in.execID, "failed", err.Error(), 0, 0)
		return nil, fmt.Errorf("engine: persist goal: %w", err)
	}
	s.emit(domain.EventSession, fmt.Sprintf("goal: %s", in.goal))
	return s, nil
}

// appendAssistant persists one assistant row (intent, final answer or
// the nudged non-answer) under the run's execution, with the step's
// usage — the legacy row shape and ID scheme (msg-<kind>-<nano>-<seq>).
func (a *Adapter) appendAssistant(in runInput, kind string, m *schema.Message, toolCalls string) {
	_, _ = a.st.Append(store.Message{ //nolint:errcheck // attempted-durable-first (the loop's posture for transcript rows)
		ID: fmt.Sprintf("msg-%s-%d-%d", kind, time.Now().UnixNano(), nextID()), SessionID: in.sessionID,
		ExecutionID: in.execID, Role: "assistant", Agent: in.agent(), Content: m.Content, Reasoning: m.ReasoningContent,
		ToolCalls: toolCalls, Provider: in.providerName, Model: in.modelID,
		PromptTokens: usageOrNull(m.ResponseMeta, true), CompletionTokens: usageOrNull(m.ResponseMeta, false),
	})
}
