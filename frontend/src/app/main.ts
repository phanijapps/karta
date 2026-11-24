import { mount } from 'svelte';
import App from './App.svelte';
import '../shared/styles/tokens.css';
import '../shared/styles/base.css';

mount(App, { target: document.getElementById('app')! });

// PWA: register the shell service worker in PRODUCTION builds only —
// dev keeps HMR clean (no cached shells fighting Vite).
if (import.meta.env.PROD && "serviceWorker" in navigator) {
  window.addEventListener("load", () => {
    void navigator.serviceWorker.register("/sw.js").catch(() => {
      /* install is best-effort; the app works without it */
    });
  });
}
