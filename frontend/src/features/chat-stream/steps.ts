// The step tree: turns the flat transcript into the shape a human
// reads — each assistant tool-call TURN is one STEP (verb + object +
// outcome); consecutive same-tool steps merge into a GROUP; a
// delegation's child turns nest under it. Pure derivation over
// messages — no contract, no backend. Pairing: intent rows carry
// tool_calls (ids), result rows carry tool_call_id.
import type { ChatMessage } from "../../shared/api/client";

export type ToolCall = {
  id: string;
  name: string;
  args: Record<string, unknown>;
};

export type CallOutcome = {
  callId: string;
  output: string;
  isError: boolean;
};

export type Step = {
  key: string;
  /** Tool name (raw) — the friendly verb derives from it. */
  tool: string;
  /** The acting agent ("root" or a sub-agent's name) — attribution. */
  agent: string;
  calls: ToolCall[];
  outcomes: CallOutcome[];
  /** Plan narration the model attached to the turn (folded detail). */
  narration: string;
  /** The turn's reasoning (folded detail — the brain). */
  reasoning: string;
  /** Delegation children: steps that ran under this delegate call. */
  children: Step[];
  /** Consecutive same-tool steps merged into this one. */
  merged: number;
  /** True while the step is the live (in-flight) turn. */
  live: boolean;
};

/** Friendly verb + object for a tool call (the readable top level). */
export function describeCall(call: ToolCall, outcome?: CallOutcome): { verb: string; object: string } {
  const a = call.args ?? {};
  const s = (v: unknown) => (typeof v === "string" ? v : "");
  switch (call.name) {
    case "ward_use":
    case "wards":
      return { verb: "Entered ward", object: s(a.name) || "vault" };
    case "ward_create":
      return { verb: "Created ward", object: s(a.name) };
    case "read":
    case "ward_read":
      return { verb: "Read", object: shortPath(s(a.path) ?? s(a.file)) };
    case "write":
    case "ward_write":
      return { verb: "Wrote", object: shortPath(s(a.path) ?? s(a.file)) };
    case "ward_list":
      return { verb: "Listed", object: "ward files" };
    case "shell":
    case "ward_shell":
      return { verb: "Ran", object: shortCmd(s(a.command)) };
    case "delegate_agent":
      return { verb: "Delegated", object: [s(a.agent), s(a.task) ? `— ${s(a.task)}` : ""].filter(Boolean).join(" ") };
    case "memory_remember":
      return { verb: "Remembered", object: "to memory" };
    case "memory_recall":
      return { verb: "Recalled", object: "from memory" };
    case "knowledge_connect":
      return { verb: "Linked", object: "knowledge" };
    case "clock.now":
      return { verb: "Checked", object: "the time" };
    case "echo":
      return { verb: "Echoed", object: shortCmd(s(a.text)) };
    default:
      if (call.name.startsWith("mcp_")) {
        if (call.name.includes("news")) return { verb: "Searched news", object: shortCmd(s(a.query)) };
        if (call.name.includes("fetch")) return { verb: "Fetched", object: shortCmd(s(a.url) ?? s(a.query)) };
        if (call.name.includes("search") || call.name.includes("llm_context")) return { verb: "Searched the web", object: shortCmd(s(a.query)) };
        return { verb: "Called", object: call.name.replace(/^mcp_/, "") };
      }
      return { verb: "Called", object: call.name };
  }
}

/** Outcome hint for a finished call (one word / number beside the verb). */
export function outcomeHint(tool: string, outcomes: CallOutcome[]): string {
  if (outcomes.length === 0) return "";
  const errs = outcomes.filter((o) => o.isError).length;
  if (errs === outcomes.length) return "error";
  if (errs > 0) return `${errs} error${errs > 1 ? "s" : ""}`;
  const last = outcomes[outcomes.length - 1].output;
  switch (true) {
    case tool === "shell" || tool === "ward_shell":
      if (/\[exit: /.test(last) && !/\[exit: exit status 0\]/.test(last)) return "non-zero exit";
      if (/\[timeout/.test(last)) return "timed out";
      const t = /\[(\d+(?:\.\d+)?(?:ms|s)) in /.exec(last);
      return t ? `exit 0 · ${t[1]}` : "exit 0";
    case tool === "write" || tool === "ward_write":
      return "ok";
    case tool.startsWith("mcp_"):
      if (/no results|bot detection|blocked/i.test(last)) return "no results";
      return "results";
    case tool === "delegate_agent":
      return "done";
    case tool === "read" || tool === "ward_read":
      return `${last.length} chars`;
    case tool === "ward_list":
      return `${last.split("\n").filter(Boolean).length} entries`;
    default:
      return "ok";
  }
}

function shortPath(p: string): string {
  if (!p) return "";
  const parts = p.split("/");
  return parts.length > 2 ? "…/" + parts.slice(-2).join("/") : p;
}

function shortCmd(c: string): string {
  if (!c) return "";
  const first = c.split("\n")[0];
  return first.length > 48 ? first.slice(0, 48) + "…" : first;
}

function parseCalls(m: ChatMessage): ToolCall[] {
  if (!m.tool_calls) return [];
  try {
    const raw = JSON.parse(m.tool_calls) as Array<{ id?: string; function?: { name?: string; arguments?: string } }>;
    return raw.map((c, i) => {
      let args: Record<string, unknown> = {};
      try {
        args = JSON.parse(c.function?.arguments ?? "{}") as Record<string, unknown>;
      } catch {
        /* malformed args: describe by name only */
      }
      return { id: c.id ?? `call-${m.id}-${i}`, name: c.function?.name ?? "tool", args };
    });
  } catch {
    return [];
  }
}

/**
 * buildSteps derives the step tree from transcript rows. Consecutive
 * same-tool steps merge (count + merged outcomes); a delegation's
 * children (rows that follow until the delegate's result lands) nest
 * under it. `liveTurnId` marks the in-flight turn's step as live.
 */
export function buildSteps(messages: ChatMessage[], liveTurnId?: string): Step[] {
  const byCall = new Map<string, CallOutcome[]>();
  for (const m of messages) {
    if (m.role !== "tool" || !m.tool_call_id) continue;
    const list = byCall.get(m.tool_call_id) ?? [];
    list.push({ callId: m.tool_call_id, output: m.content ?? "", isError: /^(error|shell:|read:|write:)/i.test(m.content ?? "") });
    byCall.set(m.tool_call_id, list);
  }
  const steps: Step[] = [];
  let delegate: Step | null = null;
  let pendingDelegateResultIds: string[] = [];
  for (const m of messages) {
    if (m.role === "assistant" && m.tool_calls) {
      const calls = parseCalls(m);
      if (calls.length === 0) continue;
      // A mixed turn (e.g. clock.now + ward_read) is named by its
      // MAJORITY tool — the stray call still lists in the expansion.
      const tally = new Map<string, number>();
      for (const c of calls) tally.set(c.name, (tally.get(c.name) ?? 0) + 1);
      let tool = calls[0].name;
      for (const [n, k] of tally) if (k > (tally.get(tool) ?? 0)) tool = n;
      const step: Step = {
        key: m.id,
        tool,
        agent: m.agent ?? "root",
        calls,
        outcomes: calls.flatMap((c) => byCall.get(c.id) ?? []),
        narration: m.content ?? "",
        reasoning: m.reasoning ?? "",
        children: [],
        merged: 1,
        live: m.id === liveTurnId,
      };
      // Nest under an open delegation until its result lands — or, with
      // attribution, under the turn's latest delegation whenever the row
      // is a child's (order-independent).
      const lastDelegate = [...steps].reverse().find((x) => x.tool === "delegate_agent");
      if ((delegate && delegate.tool === "delegate_agent" && pendingDelegateResultIds.length > 0) || (step.agent !== "root" && lastDelegate)) {
        (delegate && delegate.tool === "delegate_agent" && pendingDelegateResultIds.length > 0 ? delegate : lastDelegate)!.children.push(step);
      } else if (step.tool !== "delegate_agent" && steps.length > 0 && steps[steps.length - 1].tool === step.tool && steps[steps.length - 1].agent === step.agent && !steps[steps.length - 1].live) {
        // (Delegations never merge: each is its own step with its children.)
        const prev = steps[steps.length - 1];
        prev.calls.push(...step.calls);
        prev.outcomes.push(...step.outcomes);
        prev.merged++;
        if (step.narration) prev.narration = (prev.narration + "\n" + step.narration).trim();
        if (step.reasoning) prev.reasoning = (prev.reasoning + "\n" + step.reasoning).trim();
      } else {
        steps.push(step);
        if (step.tool === "delegate_agent") {
          delegate = step;
          pendingDelegateResultIds = calls.map((c) => c.id);
        }
      }
    } else if (m.role === "tool" && m.tool_call_id && delegate) {
      pendingDelegateResultIds = pendingDelegateResultIds.filter((id) => id !== m.tool_call_id);
      if (pendingDelegateResultIds.length === 0) delegate = null;
    } else if (m.role === "user") {
      // A user row while a delegation is OPEN is the child's injected
      // context (the caller absorbs it into this turn) — it belongs to
      // the child, so the delegation stays open. A user row with no open
      // delegation starts a new turn (the caller splits there).
      if (!delegate || pendingDelegateResultIds.length === 0) delegate = null;
    }
  }
  return steps;
}
