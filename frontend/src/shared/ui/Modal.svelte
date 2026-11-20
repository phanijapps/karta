<script lang="ts">
  import { onMount } from 'svelte';
  import type { Snippet } from 'svelte';
  let { label, kind = 'settings', onclose, children }: {
    label: string; kind?: 'settings' | 'left' | 'right'; onclose: () => void; children: Snippet;
  } = $props();
  let dialog: HTMLDialogElement;
  onMount(() => {
    const active = document.activeElement as HTMLElement | null;
    const opener = active && active !== document.body ? active : document.querySelector<HTMLElement>('[aria-label="Toggle left sidebar"]');
    dialog.showModal();
    return () => { dialog.close(); opener?.focus(); };
  });
  function trapFocus(event: KeyboardEvent) {
    if (event.key !== 'Tab') return;
    const controls = [...dialog.querySelectorAll<HTMLElement>('button:not(:disabled), input:not(:disabled), select:not(:disabled), textarea:not(:disabled), a[href], [tabindex="0"]')].filter(el => el.getClientRects().length > 0);
    const first = controls[0];
    const last = controls.at(-1);
    if (event.shiftKey && document.activeElement === first) { event.preventDefault(); last?.focus(); }
    else if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first?.focus(); }
  }
</script>

<dialog bind:this={dialog!} aria-label={label} class:settings={kind === 'settings'} class:left={kind === 'left'} class:right={kind === 'right'} onkeydown={trapFocus} oncancel={(event) => { event.preventDefault(); onclose(); }} onclick={(event) => { if (event.target === dialog) { const rect = dialog.getBoundingClientRect(); if (event.clientX < rect.left || event.clientX > rect.right || event.clientY < rect.top || event.clientY > rect.bottom) onclose(); } }}>
  <div class="modal-body">{@render children()}</div>
</dialog>

<style>
  dialog { padding: 0; color: var(--ds-color-text); background: var(--ds-color-background); border: 1px solid var(--ds-color-border); max-width: none; max-height: none; overflow: hidden; }
  dialog::backdrop { background: #0008; }
  .settings { width: calc(100vw - 4rem); height: calc(100dvh - 4rem); margin: auto; border-radius: var(--ds-radius-dialog); }
  .modal-body { height: 100%; min-height: 0; }
  .left, .right { width: min(300px, calc(100vw - 2rem)); height: 100dvh; margin: 0; }
  .right { margin-left: auto; }
  @media (max-width: 767px) { .settings { width: calc(100vw - 1rem); height: calc(100dvh - 1rem); border-radius: var(--ds-radius-composer); } }
</style>
