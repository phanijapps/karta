export const sections = ['Providers', 'MCP', 'Skills', 'Logging', 'Advanced'] as const;
export type Section = typeof sections[number];
export const providers = [
  { name: 'Local provider', type: 'OpenAI-compatible', url: 'http://localhost:11434/v1', models: 'local-model', credential: 'LOCAL_API_KEY' },
  { name: 'Custom endpoint', type: 'OpenAI-compatible', url: 'https://provider.example/v1', models: 'example-model', credential: 'PROVIDER_API_KEY' },
];
export function settingsFixtures() {
  return { providers: structuredClone(providers), level: 'Info', diagnostics: false, engine: 'eino', address: '127.0.0.1:9246', maxTokens: 32000, reserveTokens: 4096, wards: '~/.zbot/wards' };
}
export const mcps = [
  { name: 'Project tools', transport: 'stdio', value: 'example-mcp --workspace <path>', description: 'Illustrative local tool server. No process is started.' },
  { name: 'Remote tools', transport: 'Streamable HTTP', value: 'https://mcp.example/mcp', description: 'Illustrative server endpoint. No connection is made.' },
];
export const skills = [
  { name: 'Project planning', description: 'Break an idea into small, reviewable steps.', path: '~/.zbot/config/skills/project-planning/SKILL.md' },
  { name: 'Code review', description: 'Review a change for correctness and maintainability.', path: '~/.zbot/config/skills/code-review/SKILL.md' },
];
