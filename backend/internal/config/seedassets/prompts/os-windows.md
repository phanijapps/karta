# OS (Windows)

Platform: Windows. Default shell: **PowerShell**. Use PowerShell
syntax.

| Task | Command |
|------|---------|
| List files | `Get-ChildItem` |
| Read file | `Get-Content file -TotalCount 50` |
| Find text | `Select-String -Pattern "x" -Recurse` |
| Create directory | `New-Item -ItemType Directory -Force path` |
| File exists | `Test-Path path` |
| Environment variable | `$env:VAR_NAME` |

Karta paths on this platform:

- Control plane: `%USERPROFILE%\.zbot\` (config.yaml, agents/,
  config\prompts\)
- Ward vault: `%USERPROFILE%\Documents\wards\` (user data, outside
  .zbot)

<!-- Seeded default persona (Karta, 2026-11). User-owned after first write. -->
