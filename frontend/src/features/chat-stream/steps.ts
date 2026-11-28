// The step tree: turns the flat transcript into the shape a human
// reads — each assistant tool-call TURN is one STEP (verb + object +
// outcome); consecutive same-tool steps merge into a GROUP; a
// delegation's child turns nest under it. Pure derivation over
// messages — no contract, no backend. Pairing: intent rows carry
// tool_calls (ids), result rows carry tool_call_id.
import type { ChatMessage } from "../../shared/api/client";
import { isDelegationTool, delegationTarget } from './delegation';

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
  /** The acting agent ("root" or a sub-agent's name); "" on legacy rows
   *  (no attribution) — read as root, nested by adjacency. */
  agent: string;
  /** The execution the step ran under (child executions bind to their
   *  delegation; "" on legacy rows). */
  executionId: string;
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
  // Delegations come in two shapes (`delegate_agent`, `delegate_<agent>`)
  // — matched by prefix, so they sit outside the name switch.
  if (isDelegationTool(call.name)) {
    return { verb: "Delegated", object: [delegationTarget(call.name, a), s(a.task) ? `— ${taskGist(s(a.task), 60)}` : ""].filter(Boolean).join(" ") };
  }
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
    case isDelegationTool(tool):
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

export function parseCalls(m: ChatMessage): ToolCall[] {
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
  // child execution → the delegation step it bound to (identity-based nesting).
  const boundDelegates = new Map<string, Step>();
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
        agent: m.agent ?? "",
        executionId: m.execution_id ?? "",
        calls,
        outcomes: calls.flatMap((c) => byCall.get(c.id) ?? []),
        narration: m.content ?? "",
        reasoning: m.reasoning ?? "",
        children: [],
        merged: 1,
        live: m.id === liveTurnId,
      };
      // Nesting: an ATTRIBUTED child step binds to its delegation by
      // EXECUTION (the first delegate step to its agent that no other
      // execution has claimed yet; the binding sticks for that
      // execution), so order never matters and a root-attributed step
      // never nests. Legacy rows (no agent) keep adjacency: an open
      // delegation until its result lands.
      const openDelegate = delegate && isDelegationTool(delegate.tool) && pendingDelegateResultIds.length > 0 ? delegate : null;
      if (step.agent !== "root" && step.agent !== "" && step.executionId) {
        const claimed = new Set(boundDelegates.values());
        let target: Step | undefined = boundDelegates.get(step.executionId);
        if (!target) {
          // The open delegation if it is to this agent; else the nearest
          // unclaimed delegation to this agent.
          const toAgent = (x: Step) => x.calls.some((c) => isDelegationTool(c.name) && delegationTarget(c.name, c.args) === step.agent);
          if (openDelegate && toAgent(openDelegate)) target = openDelegate;
          else target = [...steps].reverse().find((x) => toAgent(x) && !claimed.has(x));
          if (target) boundDelegates.set(step.executionId, target);
        }
        if (target) target.children.push(step);
        else steps.push(step);
      } else if (step.agent === "" && openDelegate) {
        openDelegate.children.push(step);
      } else if (!isDelegationTool(step.tool) && steps.length > 0 && steps[steps.length - 1].tool === step.tool && steps[steps.length - 1].agent === step.agent && !steps[steps.length - 1].live) {
        // (Delegations never merge: each is its own step with its children.)
        const prev = steps[steps.length - 1];
        prev.calls.push(...step.calls);
        prev.outcomes.push(...step.outcomes);
        prev.merged++;
        if (step.narration) prev.narration = (prev.narration + "\n" + step.narration).trim();
        if (step.reasoning) prev.reasoning = (prev.reasoning + "\n" + step.reasoning).trim();
      } else {
        steps.push(step);
        if (isDelegationTool(step.tool)) {
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

/** One delegation as the thread shows it: the root's delegation call
 *  (`delegate_agent` or a per-agent `delegate_<agent>` tool), the agent it
 *  went to, the child's rows, and the report. */
export type DelegationGroup = {
  callId: string;
  agent: string;
  task: string;
  /** The child's execution when its rows carry it ("" on legacy rows). */
  executionId: string;
  /** The intent row (the root's assistant row carrying the call). */
  intent: ChatMessage;
  /** The child's rows: its tool-call intents and their results. */
  rows: ChatMessage[];
  /** The report the root received (the call's tool result). */
  report: string;
  /** True once the result row has landed. */
  done: boolean;
};

/**
 * delegationGroups derives one group per `delegate_agent` call from a
 * turn's rows. The child's rows are those between the intent and the
 * call's result; with attribution, rows carrying the child's execution
 * id join the group wherever they sit. Works for transcripts that
 * predate attribution (the intent's arguments name the agent).
 */
export function delegationGroups(rows: ChatMessage[]): DelegationGroup[] {
  const groups: DelegationGroup[] = [];
  const byCall = new Map<string, DelegationGroup>();
  for (const m of rows) {
    if (m.role === "assistant" && m.tool_calls) {
      let isDelegateIntent = false;
      for (const c of parseCalls(m)) {
        if (!isDelegationTool(c.name)) continue;
        isDelegateIntent = true;
        const g: DelegationGroup = {
          callId: c.id,
          agent: delegationTarget(c.name, c.args) || "agent",
          task: String(c.args.task ?? ""),
          executionId: "",
          intent: m,
          rows: [],
          report: "",
          done: false,
        };
        groups.push(g);
        byCall.set(c.id, g);
      }
      if (isDelegateIntent) continue; // the root's intent row is not a child row
    }
    if (m.role === "tool" && isDelegationTool(m.tool_name)) {
      // The result row: by its call id when supplied (an unknown id
      // matches nothing — never another delegation's result); legacy
      // rows without ids take the most recent group still waiting.
      const g = m.tool_call_id ? byCall.get(m.tool_call_id) : [...groups].reverse().find((x) => !x.done);
      if (g) {
        g.report = m.content ?? "";
        g.done = true;
      }
      continue;
    }
    if (m.role !== "user" && m.role !== "assistant" && m.role !== "tool") continue;
    if (m.agent === "root") continue; // the root's own rows never join a delegation
    if (m.agent || m.execution_id) {
      // ATTRIBUTED: only an exact agent match (and execution match once
      // bound) joins — a mismatched row is left out rather than shown
      // under another agent's pill.
      const g = groups.find((x) => x.agent === m.agent && (x.executionId === "" || x.executionId === m.execution_id) && !x.done)
        ?? groups.find((x) => x.agent === m.agent && x.executionId === m.execution_id);
      if (!g) continue;
      if (!g.executionId && m.execution_id) g.executionId = m.execution_id;
      g.rows.push(m);
      continue;
    }
    // LEGACY (no agent, no execution): adjacency — the most recent
    // group still waiting for its result. Overlapping legacy
    // delegations cannot be told apart beyond this.
    const open = [...groups].reverse().find((x) => !x.done);
    if (open) open.rows.push(m);
  }
  return groups;
}

/** A one-line gist of a delegated task: the engine's ward preamble
 *  ("Ward: <name> (you are hosted inside it; …)") dropped, whitespace
 *  collapsed, clamped. */
export function taskGist(task: string, max = 120): string {
  let t = (task ?? "").replace(/^Ward: [^\n]*?\)\s*[.;,:]?\s*/i, "").replace(/\s+/g, " ").trim();
  if (!t) t = (task ?? "").replace(/\s+/g, " ").trim();
  if (t.length > max) t = t.slice(0, max - 1).trimEnd() + "…";
  return t;
}

/** Tracks the delegations a turn has OPEN (a delegate intent whose result
 *  has not landed). Legacy rows may carry no call ids — such a result
 *  closes the NEWEST open call, the same nearest-open rule
 *  `delegationGroups` uses, so the two never disagree. A result whose id
 *  matches nothing is a no-op (it never consumes an unrelated call). */
export function openDelegationTracker() {
  const open: string[] = [];
  return {
    openIntent(calls: ToolCall[]) {
      for (const c of calls) if (isDelegationTool(c.name)) open.push(c.id);
    },
    closeResult(m: ChatMessage) {
      if (m.tool_call_id) {
        const i = open.indexOf(m.tool_call_id);
        if (i >= 0) open.splice(i, 1);
        return;
      }
      if (open.length) open.pop(); // legacy result without an id → newest open
    },
    get size() {
      return open.length;
    },
  };
}
