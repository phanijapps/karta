// Text for the analysis trail's rows (the Work tab): one line per
// analysis_* event, with the decision's cost when the terminal event
// carries it. Pure — the Timeline store supplies the events.

import type { IntentAnalysisPayload } from '../../shared/api/client';

export type AnalysisEventLike = { id: string; kind: string; text?: string; intentAnalysis?: unknown };

/** 1200 → "1.2k"; 1048576 → "1.0M" — token counts stay short in rows. */
export function fmtTokens(n: number | undefined | null): string {
  const v = Number(n ?? 0);
  if (!v) return '0';
  if (v >= 1_000_000) return `${(v / 1_000_000).toFixed(1)}M`;
  if (v >= 1000) return `${(v / 1000).toFixed(1)}k`;
  return String(v);
}

const ANALYSIS_KINDS = new Set(['analysis_started', 'analysis_completed', 'analysis_bypassed', 'analysis_failed']);

/** True for the four analysis_* kinds (started + the three terminal ones). */
export function isAnalysisKind(kind: string): boolean {
  return ANALYSIS_KINDS.has(kind);
}

/** True for the kinds that carry a decision outcome (the card renders these). */
export function isTerminalAnalysisKind(kind: string): boolean {
  return kind === 'analysis_completed' || kind === 'analysis_bypassed' || kind === 'analysis_failed';
}

/** One row's text: null when the event is not part of the trail. */
export function analysisText(ev: AnalysisEventLike): string | null {
  const ia = (ev.intentAnalysis ?? {}) as Partial<IntentAnalysisPayload>;
  const clamp = (s: string, n: number) => (s.length > n ? s.slice(0, n) + '…' : s);
  switch (ev.kind) {
    case 'analysis_started':
      return `Analysis started${ia.model ? ` · ${ia.model}` : ''}${ia.context_window ? ` · window ${fmtTokens(ia.context_window)}` : ''}`;
    case 'analysis_completed': {
      const cost = costText(ia);
      return ia.primary_intent
        ? `${ia.primary_intent} · ${ia.complexity ?? '?'} · budget ${ia.step_budget ?? '?'}${cost ? ` · ${cost}` : ''}`
        : `Analysis complete${cost ? ` · ${cost}` : ''}`;
    }
    case 'analysis_bypassed':
    case 'analysis_failed':
      return clamp(`${ev.kind === 'analysis_bypassed' ? 'Analysis bypassed' : 'Analysis failed'}${ia.reason ? ` · ${ia.reason}` : ''}`, 200);
    default:
      return null;
  }
}

/** "1.8k in / 265 out · 2960 ms · 1 attempt" from the fields present. */
export function costText(ia: Partial<IntentAnalysisPayload>): string {
  const parts: string[] = [];
  if (ia.tokens_in !== undefined || ia.tokens_out !== undefined) {
    parts.push(`${fmtTokens(ia.tokens_in)} in / ${fmtTokens(ia.tokens_out)} out`);
  }
  if (ia.duration_ms !== undefined) parts.push(`${ia.duration_ms} ms`);
  if (ia.attempts !== undefined) parts.push(`${ia.attempts} attempt${ia.attempts === 1 ? '' : 's'}`);
  return parts.join(' · ');
}
