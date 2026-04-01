# claude-session-tui

A terminal UI for browsing and managing [Claude Code](https://docs.anthropic.com/en/docs/claude-code) sessions. Browse all your past sessions across projects, preview conversations, generate AI summaries, and resume sessions in your preferred terminal.

## Features

- **Session browser** - Discovers and lists all Claude Code sessions from `~/.claude/projects/`, grouped by recency (Recent / Last Week / Older) with color coding
- **Session info** - Displays project path, session ID, modification time, message counts, git branch, and first/last meaningful user messages
- **Conversation preview** - Scrollable view of the full conversation with color-coded user/assistant messages
- **Search & filter** - Real-time fuzzy search across project names, messages, and git branches
- **AI summaries** - On-demand session summaries powered by Claude Haiku, cached in memory
- **Multi-terminal support** - Auto-detects your terminal and opens sessions natively:

  | Terminal | Tab | Split |
  |----------|-----|-------|
  | iTerm2 | New tab | Vertical split |
  | Terminal.app | New tab | New window |
  | tmux | New window | Horizontal split |
  | Kitty | New tab | New window |
  | WezTerm | New tab | Split pane |
  | Fallback | Suspends TUI, runs full-screen | Same |

## Install

Requires Go 1.21+ and [Claude Code](https://docs.anthropic.com/en/docs/claude-code) CLI.

```bash
go build -o claude-session-tui .
```

Optionally move the binary to your PATH:

```bash
mv claude-session-tui /usr/local/bin/
```

## Usage

```bash
claude-session-tui
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

## Layout

```
+------------------+-------------------------------+
|   Sessions       |   Session Info                |
|                  |   (project, path, branch,     |
|   -- Recent --   |    messages, AI summary)       |
|   (green)        |                               |
|                  +-------------------------------+
|   -- Last Week --|   Conversation Preview        |
|   (blue)         |                               |
|                  |   >>> User: ...               |
|   -- Older --    |   <<< Assistant: ...          |
|   (gray)         |                               |
+------------------+-------------------------------+
| status bar                                       |
+--------------------------------------------------+
```

## How It Works

1. **Session discovery** - Scans `~/.claude/projects/*/` for `.jsonl` session files
2. **Path decoding** - Resolves encoded directory names (e.g., `-Users-bzhai-Documents-github-workspace-cc-microsoft-teams`) back to real filesystem paths by walking the directory tree
3. **Message parsing** - Extracts user/assistant messages from JSONL, strips system metadata and XML tags, filters out slash commands and noise
4. **Terminal detection** - Checks `TMUX`, `TERM_PROGRAM`, and `KITTY_PID` environment variables to determine the best way to open sessions
5. **AI summaries** - Uses `claude -p --model haiku --bare --no-session-persistence` to generate concise session summaries without creating ghost sessions

## License

MIT
