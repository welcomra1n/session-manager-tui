# claude-session-manager-tui

A terminal UI for browsing and managing [Claude Code](https://docs.anthropic.com/en/docs/claude-code) sessions. Browse all your past sessions across projects, preview conversations, generate AI summaries, and resume sessions in your preferred terminal.

## Screenshot

```
┌ Sessions ════════════════════════┐┌ Session Info ══════════════════════════════════════┐
│(03/31 14:22) my-web-app          ││Project:     my-web-app                             │
│  add dark mode toggle to navbar  ││Path:        /Users/dev/projects/my-web-app          │
│(03/31 11:05) api-server          ││Session:     a1b2c3d4-e5f6-7890-abcd-ef1234567890   │
│  fix rate limiting on /api/v2    ││Modified:    2026-03-31 14:22:18                     │
│(03/31 09:30) infra-terraform     ││Size:        2.1 MB                                  │
│  migrate from aws to gcp module  ││Messages:    42 (18 user, 24 asst)                   │
│(03/30 16:45) mobile-app          ││Git Branch:  feature/dark-mode                       │
│  implement push notifications    ││CWD:         /Users/dev/projects/my-web-app          │
│(03/30 10:12) data-pipeline       ││                                                     │
│  debug kafka consumer lag issue  ││First msg:                                            │
│────────────────────────────────  ││  add dark mode toggle to the navbar, use tailwind    │
│(03/22 09:15) docs-site           ││                                                     │
│  update API reference for v3     ││── AI Summary ──                                     │
│(03/18 14:30) legacy-monolith     ││- Added dark mode toggle component to navbar         │
│  refactor auth middleware        ││- Implemented CSS custom properties for theme colors  │
│(03/15 11:22) cli-tool            ││- Added localStorage persistence for user preference  │
│  add --json output flag          ││- Tests passing, PR ready for review                  │
│                                  │├─────────────────────────────────────────────────────┤
│                                  ││ Conversation Preview ═══════════════════════════════ │
│                                  ││>>> User:                                             │
│                                  ││add dark mode toggle to the navbar, use tailwind      │
│                                  ││css dark: variants. persist the preference in         │
│                                  ││localStorage                                          │
│                                  ││                                                      │
│                                  ││<<< Assistant:                                        │
│                                  ││I'll add a dark mode toggle to the navbar. Let me      │
│                                  ││start by examining the current navbar component...     │
│                                  ││                                                      │
│                                  ││>>> User:                                              │
│                                  ││looks good, but can you also update the footer?        │
└──────────────────────────────────┘└──────────────────────────────────────────────────────┘
 29 sessions (iTerm2) | Enter tab | s split | i summary | / search | r refresh | q quit
```

## Features

- **Session browser** — Discovers all Claude Code sessions from `~/.claude/projects/`, split into recent (last 7 days, green) and older (gray) with a divider line
- **Session info** — Project path, session ID, modification time, message counts, git branch, first/last meaningful user messages
- **Conversation preview** — Scrollable view of the full conversation with color-coded user/assistant messages
- **Search & filter** — Real-time search across project names, messages, and git branches (`/` to activate)
- **AI summaries** — On-demand session summaries powered by Claude Haiku (`i` to generate), cached in memory
- **Multi-terminal support** — Auto-detects your terminal and opens sessions natively:

  | Terminal | Detection | Tab (`Enter`) | Split (`s`) |
  |----------|-----------|---------------|-------------|
  | iTerm2 | `TERM_PROGRAM=iTerm.app` | New tab | Vertical split |
  | Terminal.app | `TERM_PROGRAM=Apple_Terminal` | New tab | New window |
  | tmux | `TMUX` env var | New window | Horizontal split |
  | Kitty | `KITTY_PID` env var | New tab | New window |
  | WezTerm | `TERM_PROGRAM=WezTerm` | New tab | Split pane |
  | Fallback | None of above | Suspends TUI | Same |

## Install

Requires Go 1.21+ and [Claude Code](https://docs.anthropic.com/en/docs/claude-code) CLI.

```bash
git clone https://github.com/borball/claude-session-manager-tui.git
cd claude-session-manager-tui
go build -o claude-session-manager-tui .
```

Optionally move the binary to your PATH:

```bash
mv claude-session-manager-tui /usr/local/bin/
```

## Usage

```bash
claude-session-manager-tui
```

## Key Bindings

| Key | Action |
|-----|--------|
| `Enter` | Resume selected session in a new terminal tab |
| `s` | Resume selected session in a split pane |
| `i` | Generate AI summary for selected session |
| `Tab` | Switch focus between session list and conversation preview |
| `/` | Open search filter |
| `Esc` | Clear search filter |
| `r` | Refresh session list |
| `q` | Quit |

## How It Works

1. **Session discovery** — Scans `~/.claude/projects/*/` for `.jsonl` session files
2. **Path decoding** — Resolves encoded directory names (e.g., `-Users-dev-projects-my-web-app`) back to real filesystem paths by walking the directory tree, correctly handling directory names containing dashes
3. **Message parsing** — Extracts user/assistant messages from JSONL, strips system metadata and XML tags, filters out slash commands (`/model`, `/resume`) and noise
4. **Terminal detection** — Checks `TMUX`, `TERM_PROGRAM`, and `KITTY_PID` environment variables to determine the best way to open sessions
5. **AI summaries** — Uses `claude -p --model haiku --bare --no-session-persistence` to generate concise summaries without creating ghost sessions

## License

MIT
