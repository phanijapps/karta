<script lang="ts">
  import { SquarePen, Search, Folder, SlidersHorizontal, Settings, PanelLeftClose, MessageSquare, ChevronDown } from '@lucide/svelte';
  import type { Sessions } from '../entities/session/session.svelte';
  let { sessions, rail = false, ontoggle, onselect, onplaceholder, onsettings }: {
    sessions: Sessions; rail?: boolean; ontoggle: () => void; onselect: () => void;
    onplaceholder: (name: string) => void; onsettings: () => void;
  } = $props();
  let search = $state('');
  let searching = $state(false);
</script>

<nav class="sidebar" class:rail aria-label="Workspace navigation">
  <div class="brand-row"><span class="wordmark" aria-hidden="true">K<span class="brand-text">arta</span></span><span class="sr-only">Karta</span><button class="icon-button collapse" onclick={ontoggle} aria-label="Collapse left sidebar"><PanelLeftClose size={18} /></button></div>
  <div class="primary">
    <button class="nav-row" onclick={() => { sessions.newChat(); onselect(); }} title="New chat" aria-label="New chat"><SquarePen size={18} /><span>New chat</span><kbd>↗</kbd></button>
    <button class="nav-row" onclick={() => { searching = !searching; if (rail) ontoggle(); }} aria-expanded={searching} title="Search chats"><Search size={18} /><span>Search chats</span></button>
    {#if searching && !rail}<label class="search"><span class="sr-only">Filter sample chats</span><input bind:value={search} placeholder="Search sample chats" /></label>{/if}
    <button class="nav-row" onclick={() => onplaceholder('Code Wards')} title="Code Wards — coming soon"><Folder size={18} /><span>Code Wards</span><span class="todo">TODO</span></button>
    <button class="nav-row" onclick={() => onplaceholder('Configuration')} title="Configuration — coming soon"><SlidersHorizontal size={18} /><span>Configuration</span><span class="todo">TODO</span></button>
  </div>
  <div class="history"><p class="history-label">Your chats <span class="badge">Mock</span></p>
    {#each sessions.conversations.filter(chat => chat.title.toLowerCase().includes(search.toLowerCase())) as chat (chat.id)}<button class="conversation" class:active={sessions.selected === chat.id} aria-current={sessions.selected === chat.id ? 'page' : undefined} title={chat.title} onclick={() => { sessions.select(chat.id); onselect(); }}><MessageSquare size={16} /><span>{chat.title}</span></button>{/each}
    {#if !sessions.conversations.some(chat => chat.title.toLowerCase().includes(search.toLowerCase()))}<p class="muted empty-search">No matching sample chats.</p>{/if}
  </div>
  <div class="profile"><button class="nav-row" onclick={onsettings} title="Settings"><Settings size={18} /><span>Settings</span></button><div class="profile-row"><span class="profile-avatar">L</span><span class="profile-copy"><strong>Local workspace</strong><small>Shell preview</small></span><ChevronDown class="profile-chevron" size={14} /></div></div>
</nav>

<style>
  .sidebar { background: var(--ds-color-sidebar); height: 100%; display: flex; flex-direction: column; padding: var(--ds-space-3) var(--ds-space-2); font-size: var(--ds-font-nav); }
  .brand-row { display: flex; justify-content: space-between; align-items: center; min-height: 40px; padding: 0 var(--ds-space-2); margin-bottom: var(--ds-space-4); }
  .wordmark { font-size: 1.375rem; font-weight: 700; letter-spacing: -0.06em; }
  .nav-row { display: flex; align-items: center; gap: var(--ds-space-3); padding: var(--ds-space-2); width: 100%; text-align: left; min-height: 36px; font-weight: 500; }
  .nav-row span:not(.todo) { flex: 1; }
  kbd, .todo { font-size: 0.625rem; color: var(--ds-color-muted); }
  .primary { display: grid; gap: var(--ds-space-1); }
  .search { margin: var(--ds-space-2); }
  .history { flex: 1; min-height: 0; overflow-y: auto; margin-top: var(--ds-space-8); }
  .history-label { display: flex; justify-content: space-between; align-items: center; padding: 0 var(--ds-space-2); margin-bottom: var(--ds-space-3); color: var(--ds-color-muted); font-size: 0.75rem; }
  .conversation { display: flex; align-items: center; gap: var(--ds-space-2); width: 100%; text-align: left; padding: var(--ds-space-2); min-height: 36px; font-weight: 400; }
  .conversation span { overflow: hidden; white-space: nowrap; text-overflow: ellipsis; }
  .conversation :global(svg) { display: none; }
  .conversation.active { background: var(--ds-color-hover); }
  .profile { padding-top: var(--ds-space-3); }
  .profile-row { display: flex; align-items: center; gap: var(--ds-space-3); margin: var(--ds-space-3) var(--ds-space-2) var(--ds-space-2); }
  .profile-avatar { display: grid; place-items: center; width: 28px; height: 28px; border-radius: 50%; background: var(--ds-color-hover); font-weight: 600; }
  .profile-copy { display: grid; flex: 1; gap: 2px; }
  .profile-copy strong { font-size: 0.75rem; font-weight: 500; }
  small { color: var(--ds-color-muted); font-size: 0.6875rem; }
  .empty-search { padding: var(--ds-space-2); }
  .rail { padding: var(--ds-space-3) var(--ds-space-1); }
  .rail .brand-text, .rail .collapse, .rail .nav-row span, .rail kbd, .rail .history-label, .rail .conversation span, .rail .profile-copy, .rail :global(.profile-chevron) { display: none; }
  .rail .brand-row { justify-content: center; padding: 0; }
  .rail .nav-row, .rail .conversation { justify-content: center; padding: var(--ds-space-2) 0; }
  .rail .conversation :global(svg) { display: block; }
  .rail .profile-row { justify-content: center; margin: var(--ds-space-3) 0; }
</style>
