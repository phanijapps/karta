export const sections = ['Providers', 'MCP', 'Skills', 'Logging', 'Advanced'] as const;
export type Section = typeof sections[number];
// Providers, MCP, and Skills are NOT fixtures: those Settings sections
// are live daemon views (src/shared/api/client.ts). Only Logging and
// Advanced carry shell-preview drafts.
export function settingsFixtures() {
  return { level: 'Info', diagnostics: false, engine: 'eino', address: '127.0.0.1:9246', maxTokens: 32000, reserveTokens: 4096, wards: '~/.zbot/wards' };
}
