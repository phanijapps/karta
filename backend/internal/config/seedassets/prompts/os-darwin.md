# OS (macOS)

Platform: macOS. Default shell: **zsh**. Use Unix shell syntax.

| Task | Command |
|------|---------|
| List files | `ls -la` |
| Read file | `sed -n '1,50p' file` (do not cat whole files) |
| Find text | `rg "pattern" dir/` (fall back to `grep -rn`) |
| Create directory | `mkdir -p path` |
| File exists | `test -f path` |
| Environment variable | `$VAR_NAME` |

Karta paths on this platform:

- Control plane: `~/.zbot/` (config.yaml, agents/, config/prompts/)
- Ward vault: `~/Documents/wards/` (user data, outside ~/.zbot)

<!-- Seeded default persona (Karta, 2026-11). User-owned after first write. -->
