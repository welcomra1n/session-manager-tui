# CSM (Claude Session Manager) — Development Context
> Date: 2026-06-12
> Repo: welcomra1n/session-manager-tui
> Current Version: v0.3.0

## Overview

Go TUI app for managing Claude Code + Codex sessions. Single file: `main.go` (~4320 lines). Uses `tview`/`tcell` for terminal UI.

Binary name: `csm`. Distributed via Homebrew (`welcomra1n/tap`), Scoop (`welcomra1n/scoop-bucket`), and direct download.

## Tech Stack

- Go 1.25
- `github.com/rivo/tview` — TUI framework
- `github.com/gdamore/tcell/v2` — terminal cell library
- Single binary, no external dependencies at runtime

## File Structure

```
session-manager-tui/
├── main.go                          # All logic (single file)
├── go.mod / go.sum
├── .github/workflows/release.yml    # CI: build + release + Homebrew/Scoop update
└── CSM-Context-2026-06-12.md        # This file
```

## Data Locations

- Sessions: `~/.claude/projects/<encoded-path>/<uuid>.jsonl`
- Codex sessions: `~/.codex/sessions/`
- Config: `~/.claude/csm-config.json`
- Metadata: `~/.claude/csm-metadata.json` (folders, tags, colors, temp sessions)
- Aliases: `~/.claude/csm-aliases.json`
- Pins: `~/.claude/csm-pins.json`
- Unpins: `~/.claude/csm-unpins.json`
- Project aliases: `~/.claude/csm-project-aliases.json`
- Trash: `~/.claude/session-trash/`

## Core Structs

### Session
```go
type Session struct {
    ID, ProjectDir, ProjectName, SessionFile string
    ModTime      time.Time
    FileSize     int64
    MessageCount, UserMsgCount, AsstMsgCount int
    FirstUserMsg, LastUserMsg string
    GitBranch, CWD string
    Messages     []Message
    Alias        string   // user-defined name
    Selected     bool     // multi-select state
    Provider     Provider // Claude or Codex
    Entrypoint   string   // cli, claude-desktop, web
    Active       bool     // currently running
    Pinned       bool
    InputTokens  int64
    OutputTokens int64
    Loaded       bool     // detail loaded (lazy)
}
```

### Metadata
```go
type Metadata struct {
    Folders         []string            `json:"folders"`
    SessionFolders  map[string]string   `json:"session_folders"`
    SessionTags     map[string][]string `json:"session_tags"`
    FolderCollapsed map[string]bool     `json:"folder_collapsed"`
    FolderColors    map[string]string   `json:"folder_colors"`
    TempSessions    map[string]bool     `json:"temp_sessions"`
}
```

### Config
```go
type Config struct {
    ExpiryDays      int    `json:"expiry_days"`      // default 30
    RefreshInterval int    `json:"refresh_interval"` // default 10
    DefaultSort     string `json:"default_sort"`     // "date"
    DefaultTerminal string `json:"default_terminal"` // "auto"
    CompactMode     bool   `json:"compact_mode"`
}
```

## Session JSONL Format

Each line is a JSON object with `type` field:
- `user` / `assistant` — messages, assistant has `message.usage.{input_tokens, output_tokens}`
- `system` — subtypes: `turn_duration`, `compact_boundary`, `away_summary`, `api_error`
- `custom-title` — user-set session name
- `agent-name` — session auto-name
- `progress` — streaming progress
- `attachment` — skill listings, etc.
- `last-prompt` / `permission-mode` / `queue-operation` / `file-history-snapshot`

### Key system subtypes
- `compact_boundary`: `compactMetadata.{trigger, preTokens, postTokens, durationMs}`
- `away_summary`: session return summary text
- `turn_duration`: `{durationMs, messageCount, timestamp}`

## Key Bindings (All)

| Key | Action |
|-----|--------|
| Enter | Open session (claude --resume) |
| / | Search (fuzzy) |
| ? | Help panel (bottom 3-column) |
| Esc | Exit / close modal |
| Space | Toggle multi-select |
| t | Toggle pin |
| s | Sort toggle (date/size/messages) |
| r | Refresh |
| p | Toggle preview panel |
| i | Request AI summary |
| m | Rename (session alias / folder / project) |
| d | Delete (batch if multi-selected) |
| D | Batch delete selected |
| n | New folder |
| v | Move to folder (batch if selected) |
| V | Batch move (selected only) |
| g | Tag management |
| G | Tag quick filter |
| T | Create temp session (auto-delete on close) |
| P | Persist temp session → normal |
| e | Export session to markdown |
| E | Batch export selected |
| c | Compact mode toggle |
| k | Kill active session |
| o | Open session folder in Finder |
| x | Trash view |
| u | Self-update |
| C | Cycle folder color |
| < / > | Reorder folders |

## Korean Key Mapping

`korToEng` map + `korSyllableToEng()` decompose Korean IME input to English keys. Every key binding works regardless of Korean/English input mode.

## UI Language

All UI text is Korean (한국어). Status messages, labels, help text — all Korean.

## Tree Structure

```
Root (hidden)
├── 🧠 Claude (N)          ← Provider node
│   ├── 고정 📌 (N)         ← Pinned group
│   │   └── session nodes
│   └── 세션 (N)            ← Normal group
│       ├── 📁 FolderName   ← Custom folder (purple)
│       │   └── session nodes
│       └── ProjectName     ← Project group
│           └── session nodes
└── 🤖 Codex (N)            ← Provider node
```

Session node format (normal mode):
```
[check][active] MM/DD HH:MM  Title                  EP  Expiry  TokenCount  #tags  ⏳임시
```

## Architecture Notes

- **Lazy loading**: `discoverSessionsFast()` reads file metadata only. `loadSessionDetail()` parses JSONL content on demand (background goroutine).
- **Active detection**: `pgrep -afl "claude.*--resume"` to find running sessions, cached 5s.
- **Auto-refresh**: Ticker every N seconds (configurable) rediscovers sessions.
- **Temp sessions**: Created with `generateUUID()`, launched via `claude --session-id <uuid>`. Auto-deleted when closed (detected in refresh ticker).
- **Token counting**: Parsed from `message.usage` in assistant-type JSONL entries.

## Release Pipeline

Tag push (`v*`) triggers GitHub Actions:
1. Build 5 binaries (darwin arm64/amd64, linux arm64/amd64, windows amd64)
2. Create/update GitHub Release with binaries
3. Update Homebrew formula (`welcomra1n/homebrew-tap/Formula/csm.rb`)
4. Update Scoop manifest (`welcomra1n/scoop-bucket/bucket/csm.json`)

## Development Rules

1. **UI language**: Korean (한국어) for all user-facing text
2. **No sorting features** in tables (user preference)
3. **Visual style**: No colors for distinction — use shading + borders
4. **After changes**: Always provide test command (`go build -o csm . && ./csm`)
5. **Confirm modals**: Original shortcut key pressed again = confirm
6. **Button labels**: Never wrap (`white-space: nowrap` equivalent)
7. **Fill empty space**: If empty space appears, fill it and reduce pages

## Implemented Features (v0.3.0)

- Session discovery (Claude + Codex)
- Tree view with provider/project/folder grouping
- Fuzzy search with Korean decomposition
- Custom folders with colors, ordering, collapse
- Session tags with autocomplete, quick filter
- Pin/unpin (syncs with Codex desktop)
- Temp sessions (auto-delete on close, convert to normal)
- Multi-select batch operations (delete, move, export)
- Session rename/alias
- AI summary generation
- Preview panel
- Compact mode
- Trash with restore/permanent delete
- Export to markdown
- Self-update mechanism
- Platform-specific update notice (brew/scoop/direct)
- Token usage display (input + output per session)
- 30-day expiry tracking with icons
- Active session detection (local + SSH)
- Kill active session
- Help panel (bottom 3-column, Claude Code style)

---

## Phase 1 TODO: Cost Tracker + Compaction Alert

### 1. Compaction Alert
- Parse `compact_boundary` entries from session JSONL
- Track `preTokens` accumulation rate
- Show estimated time until next compaction in status bar or session detail
- Show compaction history (count, last compaction time) per session

### 2. Cost Tracker Enhancement
- Already parsing `input_tokens` / `output_tokens` per session
- Add project-level token aggregation
- Add folder/team-level token aggregation
- Show daily/weekly totals in status bar or dedicated view

## Phase 2 TODO: Agent View Integration

### 1. Agent Worker Dashboard
- Detect `/bg` background sessions
- Show worker status (running/waiting/completed/error)
- Real-time token consumption per worker
- Enter worker session from csm (`claude --resume`)

### 2. Task History Viewer
- Parse shared task lists (format TBD — needs Agent View data structure investigation)
- Track which session completed which task
- Project-level task completion stats

### 3. Session Group → Team
- Extend folder concept to "team"
- Launch multiple sessions in folder as `/bg` workers
- Team-level token totals, execution time aggregation

## Phase 3 TODO: Desktop App (Future)

### Stack: Wails (Go + WebView)
- Reuse Go backend logic from csm-core module
- Frontend: HTML/CSS/JS
- Notch overlay UI: hidden at top, slides down on hover
- Frameless window + transparent background + mouse area detection
- Cross-platform: .app (macOS) / .exe (Windows) / AppImage (Linux)

### Repo Structure (planned)
```
welcomra1n/csm-core              ← shared Go logic
welcomra1n/session-manager-tui   ← TUI (tview)
welcomra1n/csm-desktop           ← GUI (Wails)
```
