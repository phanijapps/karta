// The eternal chat session: transcript rows from the daemon, cursor
// sync state, and the selected model. Fixture conversations are retired
// — there is exactly one conversation and it lives forever.
import { syncChatMessages, type ChatMessage } from '../../shared/api/client';

export function createSessions() {
  let messages = $state<ChatMessage[]>([]);
  let cursor = $state(0);
  let model = $state(''); // catalog-backed picker sets this
  let loading = $state(true);
  let sendError = $state('');

  async function load() {
    loading = true;
    sendError = '';
    try {
      messages = [];
      cursor = 0;
      // Catch-up loop: page until not truncated.
      for (;;) {
        const page = await syncChatMessages(cursor, 200);
        messages = [...messages, ...page.messages];
        cursor = page.nextCursor;
        if (!page.truncated) break;
      }
    } catch {
      sendError = 'daemon';
    } finally {
      loading = false;
    }
  }

  // Reconcile from the last contiguously delivered cursor — after every
  // send outcome, failures included, so persisted rows always appear.
  async function reconcile() {
    try {
      for (;;) {
        const page = await syncChatMessages(cursor, 200);
        // Merge by stable id (skip rows we already hold).
        const seen = new Set(messages.map((m) => m.id));
        messages = [...messages, ...page.messages.filter((m) => !seen.has(m.id))];
        cursor = Math.max(cursor, page.nextCursor);
        if (!page.truncated) break;
      }
    } catch {
      // next load() surfaces daemon state
    }
  }

  return {
    load,
    reconcile,
    get messages() { return messages; },
    get cursor() { return cursor; },
    get model() { return model; },
    set model(value: string) { model = value; },
    get loading() { return loading; },
    get sendError() { return sendError; },
    set sendError(value: string) { sendError = value; },
  };
}

export type Sessions = ReturnType<typeof createSessions>;
