/**
 * Delegation tools on the wire. Two shapes share one prefix:
 *
 *  - `delegate_agent` — the dispatch form (the legacy loop, and older
 *    rows): the target agent rides the call's `agent` argument.
 *  - `delegate_<agent>` — the per-agent task tools of the ADK spine
 *    (engine-adk-spine, decision D1): the target is the name's suffix,
 *    the only argument is `task`.
 *
 * Rows and events that predate the spine keep working; a session that
 * runs on the spine shows the same pills, groups and slideouts.
 */
export const DISPATCH_DELEGATE_TOOL = 'delegate_agent';
const DELEGATE_PREFIX = 'delegate_';

export function isDelegationTool(name: string | undefined | null): boolean {
  return name === DISPATCH_DELEGATE_TOOL || (name?.startsWith(DELEGATE_PREFIX) ?? false);
}

/** The agent a delegation call goes to ("" when the call names none). */
export function delegationTarget(name: string, args: Record<string, unknown> | undefined | null): string {
  if (name === DISPATCH_DELEGATE_TOOL) return String(args?.agent ?? '');
  return name.slice(DELEGATE_PREFIX.length);
}
