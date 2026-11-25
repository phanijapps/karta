package intent

import "math"

// The one safety valve's arithmetic (intent-analyst-agent S1, design
// D6): the ~4 chars/token estimate a decision is measured with, and the
// per-kind packet caps it shrinks when the composed prompt would exceed
// window − OutputReserve. Routine decisions never touch these — the
// default caps are the packet's shape (plan §3.4: "catalog caps stay
// 20/20/20/64"); only a prompt that cannot fit the model's window does.

// estimateTokens is the deliberate ~4 chars/token approximation used
// only to decide whether the valve fires (never reported as usage).
func estimateTokens(chars int) int { return (chars + 3) / 4 }

// floorCaps are the valve's minimums — a packet below these cannot
// carry a decision, so the request echo gives way instead.
var floorCaps = Caps{Skills: 2, Agents: 2, MCPs: 2, Wards: 4}

// Caps are the packet's per-kind bounds.
type Caps struct{ Skills, Agents, MCPs, Wards int }

// DefaultCaps returns the shipped packet caps (agentzero's catalog
// numbers — bounded per kind so the prompt stays small regardless of
// roster size; wards the widest, ward selection being the packet's
// highest-stakes decision).
func DefaultCaps() Caps {
	return Caps{Skills: PacketCapSkills, Agents: PacketCapAgents, MCPs: PacketCapMCPs, Wards: PacketCapWards}
}

// Shrink scales every cap toward budget/est, always making progress
// (a ratio near 1 still halves), never below the floors.
func (c Caps) Shrink(est, budget int) Caps {
	ratio := float64(budget) / float64(est)
	if ratio <= 0 || math.IsNaN(ratio) {
		ratio = 0.5
	}
	next := func(cap, floor int) int {
		n := int(float64(cap) * ratio)
		if n >= cap {
			n = cap / 2
		}
		if n < floor {
			n = floor
		}
		return n
	}
	return Caps{
		Skills: next(c.Skills, floorCaps.Skills), Agents: next(c.Agents, floorCaps.Agents),
		MCPs: next(c.MCPs, floorCaps.MCPs), Wards: next(c.Wards, floorCaps.Wards),
	}
}

// AtFloors reports whether every cap sits at its floor.
func (c Caps) AtFloors() bool { return c == floorCaps }
