package chat

import "zbot/backend/internal/tools"

// SetPersona wires the composed system prompt (static sections only —
// chat is never analyzed and never provisioned skills). An ERROR from
// fn FAILS the exchange before the model call: chat never runs on an
// accidentally empty prompt. Nil restores the pre-composition
// behavior (no persona message).
func (s *Service) SetPersona(fn func() (string, error)) { s.persona = fn }

// SetExtraTools mounts additional tool planes on every exchange (the
// ward plane: wards index + structural + file tools).
func (s *Service) SetExtraTools(fn func() []tools.Tool) { s.extraTools = fn }

// SetActiveWard wires the Active Ward section renderer (composed after
// the persona when the session hosts a ward).
func (s *Service) SetActiveWard(fn func() string) { s.activeWard = fn }

// staticProvider serves a fixed tool list (mounting extra planes like
// the ward tools without a registry).
type staticProvider struct{ list []tools.Tool }

// Tools returns the fixed list.
func (p staticProvider) Tools() []tools.Tool { return p.list }
