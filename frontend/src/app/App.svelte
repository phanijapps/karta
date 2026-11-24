<script lang="ts">
  import { onMount, tick } from 'svelte';
import { clearChat } from '../shared/api/client';
  import { PanelLeft, PanelRight, Sun, Moon, Trash2 } from '@lucide/svelte';
  import Sidebar from './Sidebar.svelte';
  import ContextPanel from './ContextPanel.svelte';
  import Chat from '../features/chat-stream/Chat.svelte';
  import Schedules from '../features/schedules/Schedules.svelte';
  import Wards from '../features/wards/Wards.svelte';
  import Settings from '../features/settings/Settings.svelte';
  import Modal from '../shared/ui/Modal.svelte';
  import { createSessions } from '../entities/session/session.svelte';
  import ModelPicker from '../shared/ui/ModelPicker.svelte';
  const sessions = createSessions();
  let width = $state(1440);
  let leftExpanded = $state(true);
  let rightExpanded = $state(false);
  let drawer = $state<'left' | 'right' | null>(null);
  let settings = $state(false);
  let confirmClear = $state(false);
  let view = $state<'chat' | 'schedules' | 'wards'>('chat');
  // Browser-only SPA (no SSR): read the preference synchronously so the
  // first paint matches the user's scheme instead of flashing dark.
  let theme = $state<'light' | 'dark'>(matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light');
  let main: HTMLElement;
  const leftDocked = $derived(width >= 768);
  const rightDocked = $derived(width >= 1280);
  onMount(() => {
    let previousWidth = window.innerWidth;
    const resize = () => {
      const next = window.innerWidth;
      if ((previousWidth < 768) !== (next < 768) || (previousWidth < 1280) !== (next < 1280)) drawer = null;
      width = next; previousWidth = next;
    };
    resize(); window.addEventListener('resize', resize);
    return () => window.removeEventListener('resize', resize);
  });
  $effect(() => { document.documentElement.dataset.theme = theme; });
  function toggleLeft() { if (leftDocked) leftExpanded = !leftExpanded; else drawer = drawer === 'left' ? null : 'left'; }
  function toggleRight() { if (rightDocked) rightExpanded = !rightExpanded; else drawer = drawer === 'right' ? null : 'right'; }
  async function showChat() { drawer = null; view = 'chat'; await tick(); main.focus(); }
  async function showSession(id: string) { drawer = null; view = 'chat'; await sessions.select(id); await tick(); main.focus(); }
  async function showSchedules() { drawer = null; view = 'schedules'; await tick(); main.focus(); }
  async function showWards() { drawer = null; view = 'wards'; await tick(); main.focus(); }
  async function openSettings() { drawer = null; await tick(); settings = true; }
</script>

<a class="skip-link" href="#workspace">Skip to conversation</a>
<div class="workspace" class:left-rail={!leftExpanded} class:right-open={rightExpanded}>
  {#if leftDocked}<div class="left-panel" id="left-sidebar"><Sidebar {sessions} rail={!leftExpanded} ontoggle={toggleLeft} onselect={showSession} onsettings={openSettings} onschedules={showSchedules} onwards={showWards} /></div>{/if}
  <div class="main-column">
    <header class="workspace-header">
      <button class="icon-button" onclick={toggleLeft} aria-label="Toggle left sidebar" aria-expanded={leftDocked ? leftExpanded : drawer === 'left'} aria-controls={leftDocked || drawer === 'left' ? 'left-sidebar' : undefined}><PanelLeft size={20} strokeWidth={1.5} /></button>
      <div class="header-model"><ModelPicker id="header-model" label="Header model" bind:value={sessions.model} /></div>
      <div class="header-actions"><button class="icon-button" onclick={() => (confirmClear = true)} aria-label="Clear conversation" title="Clear conversation (deletes all messages)"><Trash2 size={18} strokeWidth={1.6} /></button><button class="icon-button" onclick={() => theme = theme === 'dark' ? 'light' : 'dark'} aria-label={theme === 'dark' ? 'Switch to light theme' : 'Switch to dark theme'}>{#if theme === 'dark'}<Sun size={18} />{:else}<Moon size={18} />{/if}</button><button class="icon-button" onclick={toggleRight} aria-label="Toggle right sidebar" aria-expanded={rightDocked ? rightExpanded : drawer === 'right'} aria-controls={rightDocked && rightExpanded || drawer === 'right' ? 'right-sidebar' : undefined}><PanelRight size={20} strokeWidth={1.5} /></button></div>
    </header>
    <main id="workspace" bind:this={main!} tabindex="-1">
      {#if view === 'schedules'}<Schedules onopenSession={() => { view = 'chat'; }} />{:else if view === 'wards'}<Wards />{:else}<Chat {sessions} />{/if}
    </main>
  </div>
  {#if rightDocked && rightExpanded}<div class="right-panel" id="right-sidebar"><ContextPanel {sessions} onclose={() => rightExpanded = false} /></div>{/if}
</div>
{#if drawer === 'left'}<Modal label="Workspace navigation" kind="left" onclose={() => drawer = null}><div id="left-sidebar" class="drawer-content"><Sidebar {sessions} ontoggle={() => drawer = null} onselect={showSession} onsettings={openSettings} onschedules={showSchedules} onwards={showWards} /></div></Modal>{:else if drawer === 'right'}<Modal label="Session context" kind="right" onclose={() => drawer = null}><div id="right-sidebar" class="drawer-content"><ContextPanel {sessions} onclose={() => drawer = null} /></div></Modal>{/if}
{#if confirmClear}
  <div class="form-backdrop" role="presentation" onclick={(e) => { if (e.target === e.currentTarget) confirmClear = false; }}>
    <div class="confirm-card" role="alertdialog" aria-label="Clear conversation">
      <h3>Clear this conversation?</h3>
      <p class="muted">All messages in this session will be permanently deleted from your local database. This cannot be undone.</p>
      <div class="confirm-actions">
        <button class="cancel" onclick={() => (confirmClear = false)}>Cancel</button>
        <button class="danger" onclick={async () => { const sid = sessions.activeId; if (await clearChat(sid)) { confirmClear = false; sessions.clearedLocally(sid); } else { sessions.sendError = 'clear failed — the transcript is intact'; } }}>Clear everything</button>
      </div>
    </div>
  </div>
{/if}
{#if settings}<Settings onclose={() => settings = false} />{/if}

<style>
  .workspace { height: 100dvh; display: grid; grid-template-columns: 260px minmax(0, 1fr); background: var(--ds-color-background); color: var(--ds-color-text); }
  .workspace.left-rail { grid-template-columns: 44px minmax(0, 1fr); }
  .workspace.right-open { grid-template-columns: 260px minmax(0, 1fr) 300px; }
  .workspace.left-rail.right-open { grid-template-columns: 44px minmax(0, 1fr) 300px; }
  .left-panel, .right-panel, .main-column { min-width: 0; min-height: 0; }
  .main-column { display: flex; flex-direction: column; }
  .workspace-header { display: flex; align-items: center; gap: var(--ds-space-2); height: 56px; flex-shrink: 0; padding: 0 var(--ds-space-4); }
  .header-model { display: flex; align-items: center; gap: var(--ds-space-1); min-width: 0; }
  .header-actions { margin-left: auto; display: flex; gap: var(--ds-space-1); }
  main { flex: 1; min-height: 0; outline: none; }
  .drawer-content { height: 100%; }
  @media (min-width: 768px) and (max-width: 1279px) { .workspace.right-open { grid-template-columns: 260px minmax(0, 1fr); } .workspace.left-rail.right-open { grid-template-columns: 44px minmax(0, 1fr); } }
  @media (max-width: 767px) { .workspace, .workspace.left-rail, .workspace.right-open, .workspace.left-rail.right-open { grid-template-columns: minmax(0, 1fr); } .workspace-header { padding: 0 var(--ds-space-2); } }

  .form-backdrop { position: fixed; inset: 0; background: rgb(0 0 0 / 0.45); display: grid; place-items: center; z-index: 10; }
  .confirm-card { background: var(--ds-color-background); border: 1px solid var(--ds-color-border); border-radius: var(--ds-radius-dialog); padding: var(--ds-space-6); width: min(24rem, 90vw); display: grid; gap: var(--ds-space-4); }
  .confirm-card h3 { font-size: 1.05rem; font-weight: 600; }
  .confirm-actions { display: flex; justify-content: flex-end; gap: var(--ds-space-2); }
  .confirm-actions button { padding: var(--ds-space-2) var(--ds-space-4); min-height: 36px; border-radius: var(--ds-radius-control); font-size: var(--ds-font-nav); cursor: pointer; }
  .cancel { border: 1px solid var(--ds-color-border); background: var(--ds-color-background); color: var(--ds-color-text); }
  .danger { border: 1px solid #b3261e55; background: #b3261e; color: white; }
</style>
