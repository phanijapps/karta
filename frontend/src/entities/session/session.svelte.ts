export interface Message { id: number; role: 'user' | 'assistant'; text: string }
export interface Conversation { id: number; title: string; messages: Message[] }
const fixtures: Conversation[] = [
  { id: 1, title: 'A workspace that stays yours', messages: [
    { id: 1, role: 'user', text: 'Help me set up a workspace for my next project.' },
    { id: 2, role: 'assistant', text: 'Let’s make room for your next idea.\n\nStart with a goal, then give it a place to grow. Your conversations, configuration, and project context will live together in a workspace you own.\n\nHere’s a simple starting point:\n\n1. Describe what you want to build.\n2. Add the context that matters.\n3. Work through the next small step.\n\nWhat are you working on?' },
  ] },
  { id: 2, title: 'Plan a small, useful first release', messages: [
    { id: 3, role: 'user', text: 'How do I keep a first release focused?' },
    { id: 4, role: 'assistant', text: 'Pick one useful outcome and a clear stopping point.\n\nWrite down what a person should be able to do, keep everything else out of the first release, and test the complete path before adding more.\n\nThis is a sample conversation, not a live agent response.' },
  ] },
];

export function createSessions() {
  let conversations = $state<Conversation[]>(structuredClone(fixtures));
  let selected = $state(1);
  let model = $state(''); // catalog-backed picker sets this; no fixture default
  let nextId = 5;
  const active = () => conversations.find((chat) => chat.id === selected)!;
  return {
    get conversations() { return conversations; },
    get selected() { return selected; },
    get active() { return active(); },
    get model() { return model; },
    set model(value: string) { model = value; },
    select(id: number) { selected = id; },
    newChat() {
      const id = nextId++;
      conversations = [...conversations, { id, title: 'New chat', messages: [] }];
      selected = id;
    },
    send(text: string) {
      const trimmed = text.trim();
      if (!trimmed) return;
      const chat = active();
      if (!chat.messages.length) chat.title = trimmed.slice(0, 40);
      chat.messages.push({ id: nextId++, role: 'user', text: trimmed });
      chat.messages.push({ id: nextId++, role: 'assistant', text: 'This is a simulated reply. Your message stays in this page’s memory. Live agents, tools, and provider connections will arrive in a future update.' });
    },
  };
}
export type Sessions = ReturnType<typeof createSessions>;
