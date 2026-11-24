<script lang="ts">
  import { PanelLeftClose, MessageSquare, Folder, FolderPlus, Settings, CalendarClock, FlaskConical } from '@lucide/svelte';
  import type { Sessions, SessionGroups } from '../entities/session/session.svelte';
  import type { SessionView } from '../shared/api/client';

  let {
    sessions, rail = false, ontoggle, onselect, onselectWard, onsettings, onschedules, onwards,
  }: {
    sessions: Sessions; rail?: boolean; ontoggle: () => void;
    onselect: (id: string) => void; onselectWard: (ward: string) => void;
    onsettings: () => void; onschedules: () => void; onwards: () => void;
  } = $props();

  let creating = $state(false);

  const groups = $derived<SessionGroups>(sessions.groups);

  function timeHint(iso: string): string {
    const ms = Date.now() - new Date(iso).getTime();
    if (Number.isNaN(ms)) return '';
    const mins = Math.floor(ms / 60_000);
    if (mins < 1) return 'now';
    if (mins < 60) return `${mins}m ago`;
    const hours = Math.floor(mins / 60);
    if (hours < 24) return `${hours}h ago`;
    return `${Math.floor(hours / 24)}d ago`;
  }

  async function newResearch() {
    if (creating) return;
    creating = true;
    try {
      if (await sessions.newResearch()) {
        // Navigate to the fresh shell: the callback closes drawers,
        // switches the main view to the chat surface, and focuses it.
        onselect(sessions.activeId);
      }
    } finally {
      creating = false;
    }
  }
</script>

<nav class="sidebar" class:rail aria-label="Workspace navigation">
  <div class="brand-row">
    <span class="wordmark" aria-hidden="true">K<span class="brand-text">arta</span></span>
    <span class="sr-only">Karta</span>
    <button class="icon-button collapse" onclick={ontoggle} aria-label="Collapse left sidebar"><PanelLeftClose size={18} /></button>
  </div>

  <ul class="sessions" aria-label="Sessions">
    <!-- Chat: the one persistent quick-chat session -->
    <li aria-labelledby="chat-group"><p class="group-label" id="chat-group">Chat</p>
    <ul class="group">
      {#if groups.chat}
        <li>
          <button class="session" class:active={sessions.activeId === groups.chat.id} aria-current={sessions.activeId === groups.chat.id ? 'true' : undefined} onclick={() => onselect(groups.chat!.id)} title="Quick Chat — always here, lean mode">
            <span class="icon chat"><MessageSquare size={16} aria-hidden="true" /></span>
            <span class="meta"><span class="name">Quick Chat</span><span class="sub">Always here · lean mode</span></span>
          </button>
        </li>
      {/if}
    </ul></li>

    <!-- Ward Bots: every ward renders a row — its live warden session,
         or the ward itself (select → ensure the warden) -->
    <li aria-labelledby="ward-group"><p class="group-label" id="ward-group">Ward Bots</p>
    <ul class="group">
      {#each groups.wardBots as row (row.ward)}
        <li>
          {#if row.kind === 'session'}
            <button class="session" class:active={sessions.activeId === row.session.id} aria-current={sessions.activeId === row.session.id ? 'true' : undefined} onclick={() => onselect(row.session.id)} title={row.ward}>
              <span class="icon ward"><Folder size={16} aria-hidden="true" /></span>
              <span class="meta"><span class="name">{row.ward}</span><span class="sub">{timeHint(row.session.updated_at)}</span></span>
            </button>
          {:else}
            <button class="session" title={`${row.ward} — start its warden`} onclick={() => onselectWard(row.ward)}>
              <span class="icon ward"><Folder size={16} aria-hidden="true" /></span>
              <span class="meta"><span class="name">{row.ward}</span><span class="sub">warden · tap to open</span></span>
            </button>
          {/if}
        </li>
      {:else}
        <li class="empty">No wards in the vault</li>
      {/each}
    </ul></li>

    <!-- Research: shell sessions without a ward -->
    <li aria-labelledby="research-group"><p class="group-label" id="research-group">Research</p>
    <ul class="group">
      {#each groups.research as s (s.id)}
        {#if s.id !== 'default'}
          <li>
            <button class="session" class:active={sessions.activeId === s.id} aria-current={sessions.activeId === s.id ? 'true' : undefined} onclick={() => onselect(s.id)} title={s.title ?? 'Research session'}>
              <span class="icon research"><FlaskConical size={16} aria-hidden="true" /></span>
              <span class="meta"><span class="name">{s.title ?? 'Research'}</span><span class="sub">Shell — no ward yet · {timeHint(s.updated_at)}</span></span>
            </button>
          </li>
        {/if}
      {:else}
        <li class="empty">No open research</li>
      {/each}
    </ul></li>
  </ul>

  <button class="new-research" onclick={() => void newResearch()} disabled={creating} aria-label="New Research" title="New Research">
    <FolderPlus size={14} aria-hidden="true" /><span class="brand-text">New Research</span>
  </button>

  <div class="nav-footer">
    <button class="nav-row" onclick={onwards} title="Wards — your documented-knowledge vault"><Folder size={18} /><span>Wards</span></button>
    <button class="nav-row" onclick={onschedules} title="Schedules"><CalendarClock size={18} /><span>Schedules</span></button>
    <button class="nav-row" onclick={onsettings} title="Settings"><Settings size={18} /><span>Settings</span></button>
  </div>
</nav>

<style>
  .sidebar { background: var(--ds-color-sidebar); height: 100%; display: flex; flex-direction: column; padding: var(--ds-space-3) var(--ds-space-2); font-size: var(--ds-font-nav); min-height: 0; }
  .brand-row { display: flex; justify-content: space-between; align-items: center; min-height: 40px; padding: 0 var(--ds-space-2); margin-bottom: var(--ds-space-3); }
  .wordmark { font-size: 1.375rem; font-weight: 700; letter-spacing: -0.06em; }
  .sessions { flex: 1; min-height: 0; overflow-y: auto; display: grid; gap: var(--ds-space-2); align-content: start; margin: 0; padding: 0; list-style: none; }
  .group { display: grid; gap: 2px; margin: 0; padding: 0; list-style: none; }
  .group-label { display: flex; align-items: center; padding: var(--ds-space-3) var(--ds-space-2) var(--ds-space-1); color: var(--ds-color-muted); font-size: 0.68rem; text-transform: uppercase; letter-spacing: 0.08em; margin: 0; }
  .session { display: flex; align-items: center; gap: var(--ds-space-2); width: 100%; text-align: left; padding: var(--ds-space-2); min-height: 40px; font-weight: 400; border-radius: 8px; cursor: pointer; border: 1px solid transparent; }
  .session:hover { background: var(--ds-color-hover); }
  /* The monochrome light theme darkens any primary mix past AA for
   * the muted sub-line — the active row uses the hover surface plus an
   * inset accent stripe instead (opaque, contrast-safe). */
  .session.active { background: var(--ds-color-hover); box-shadow: inset 2px 0 0 var(--ds-color-primary); }
  .icon { display: grid; place-items: center; width: 28px; height: 28px; border-radius: 8px; flex-shrink: 0; background: var(--ds-color-hover); border: 1px solid var(--ds-color-border); color: var(--ds-color-muted); }
  .icon.ward { color: var(--ds-color-success, #22c55e); border-color: color-mix(in oklch, var(--ds-color-success, #22c55e) 25%, transparent); }
  .icon.research { color: var(--ds-color-primary, #6366f1); border-color: color-mix(in oklch, var(--ds-color-primary, #6366f1) 25%, transparent); }
  .meta { display: grid; gap: 1px; min-width: 0; flex: 1; }
  .name { font-size: 0.82rem; font-weight: 500; overflow: hidden; white-space: nowrap; text-overflow: ellipsis; }
  .sub { font-size: 0.68rem; color: var(--ds-color-muted); overflow: hidden; white-space: nowrap; text-overflow: ellipsis; }
  .empty { padding: var(--ds-space-1) var(--ds-space-2); font-size: 0.72rem; color: var(--ds-color-muted); margin: 0; list-style: none; }
  .new-research { display: inline-flex; align-items: center; justify-content: center; gap: var(--ds-space-2); margin: var(--ds-space-2); padding: var(--ds-space-2); min-height: 36px; border-radius: 8px; border: 1px solid color-mix(in oklch, var(--ds-color-primary, #6366f1) 50%, transparent); background: color-mix(in oklch, var(--ds-color-primary, #6366f1) 12%, transparent); color: var(--ds-color-primary, #6366f1); font-size: 0.8rem; font-weight: 500; cursor: pointer; }
  .new-research:hover { background: var(--ds-color-primary, #6366f1); color: var(--ds-color-on-primary, #fff); }
  .new-research:disabled { opacity: 0.5; cursor: wait; }
  .nav-footer { display: grid; gap: var(--ds-space-1); padding-top: var(--ds-space-2); border-top: 1px solid var(--ds-color-border); }
  .nav-row { display: flex; align-items: center; gap: var(--ds-space-3); padding: var(--ds-space-2); width: 100%; text-align: left; min-height: 36px; font-weight: 500; color: var(--ds-color-muted); }
  .nav-row:hover { background: var(--ds-color-hover); color: var(--ds-color-text); }
  .nav-row span, .session .meta { flex: 1; }
  .rail { padding: var(--ds-space-3) var(--ds-space-1); }
  .rail .brand-text, .rail .collapse, .rail .nav-row span, .rail .group-label, .rail .session .meta, .rail .empty { display: none; }
  .rail .brand-row { justify-content: center; padding: 0; }
  .rail .nav-row, .rail .session { justify-content: center; padding: var(--ds-space-2) 0; }
  .rail .new-research { padding: var(--ds-space-2) 0; }
</style>
