package main

import (
	"context"
	"log"

	"zbot/backend/chitta"
	einoengine "zbot/backend/internal/engine/eino"

	"zbot/backend/internal/chat"
	"zbot/backend/internal/config"
	"zbot/backend/internal/domain"
	"zbot/backend/internal/registry"
	"zbot/backend/internal/sysprompt"
	zbotstore "zbot/backend/internal/store"
)

// wireSessionModel wires the add-chat-modes session plumbing: the
// ward binder (every ward enter in the engine updates the session row
// — the sidebar re-groups; restarts re-enter from the binding) and the
// research path (research sessions run the engine pipeline in their
// own transcript — the chat IS the research surface).
func wireSessionModel(chatSvc *chat.Service, eng *einoengine.Adapter, runEngine domain.AgentEngine, db *zbotstore.Store) {
	eng.SetWardBinder(func(sessionID, ward string) {
		if err := db.SetSessionWard(sessionID, ward); err != nil {
			log.Printf("zbotd: session %s ward binding %q failed: %v", sessionID, ward, err)
		}
	})
	wireResearchPath(chatSvc, eng, runEngine, db)
}

// wireResearchPath routes research-SESSION sends through the engine's
// full pipeline (the session model, add-chat-modes): the wrapped engine
// (intent analysis → composed prompt → bounded run) executes the send
// in the SAME session's transcript; the outcome is every row the run
// appended. The exchange completes as a unit — the engine pipeline is
// not a token stream.
func wireResearchPath(chatSvc *chat.Service, eng *einoengine.Adapter, runEngine domain.AgentEngine, db *zbotstore.Store) {
	chatSvc.SetResearchPath(func(ctx context.Context, sessionID, text, providerName, model string) chat.SendOutcome {
		before, err := db.LatestSeq(sessionID)
		if err != nil {
			return chat.SendOutcome{Err: err}
		}
		// Restart recovery: a session whose row carries a ward binding
		// re-enters it on the fresh host BEFORE the run (the binding is
		// durable; the in-memory host is not). A ward that no longer
		// exists self-heals to unbound.
		if sess, serr := db.Session(sessionID); serr == nil && sess != nil && sess.Ward != "" {
			if werr := eng.EnsureWard(ctx, sessionID, sess.Ward); werr != nil {
				log.Printf("zbotd: session %s ward %q unavailable (%v) — unbinding", sessionID, sess.Ward, werr)
				if uerr := db.SetSessionWard(sessionID, ""); uerr != nil {
					log.Printf("zbotd: session %s unbind failed: %v", sessionID, uerr)
				}
			}
		}
		events, err := runEngine.RunGoal(ctx, domain.GoalRequest{
			SessionID: sessionID, Goal: text, Model: providerName + "/" + model,
		})
		if err != nil {
			return chat.SendOutcome{Err: err}
		}
		for range events { // drain to completion — the transcript is the truth
		}
		rows, err := db.Replay(sessionID, before, 500)
		if err != nil {
			return chat.SendOutcome{Err: err}
		}
		end := before
		for _, r := range rows {
			if r.Seq > end {
				end = r.Seq
			}
		}
		return chat.SendOutcome{Rows: rows, EndCursor: end}
	})
}

// wireLeanChat wires the quick-chat (chat-type session) composition:
// the LEAN persona drops custom sections (research-protocol et al) —
// a broken composed prompt FAILS the exchange — and memory recall
// (chitta fused recall, minus resource-index records) injects as the
// ## Context section. The session type gates the PIPELINE, never the
// knowledge.
func wireLeanChat(chatSvc *chat.Service, cfg config.Config, reg *registry.Registry, chittaSys *chitta.System) {
	leanCfg := cfg.SystemPrompt
	leanCfg.Custom = nil
	chatSvc.SetPersona(func() (string, error) { return sysprompt.Compose(leanCfg, reg.Snapshot()) })
	chatSvc.SetContext(func(ctx context.Context, text string) []string {
		page, err := chittaSys.Recall(ctx, domain.RecallRequest{Query: text, Limit: 6}, domain.ChittaHostContext{Source: domain.SourceAgent, AgentID: "root"})
		if err != nil {
			return nil
		}
		var hits []string
		for _, h := range page.Items {
			if h.Record.Kind.IsResourceKind() {
				continue
			}
			hits = append(hits, h.Record.Content)
		}
		return hits
	})
}
