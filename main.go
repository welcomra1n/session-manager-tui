package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// ── Version & update check ──────────────────────────────────────────────────

const currentVersion = "0.1.0"

type githubRelease struct {
	TagName string `json:"tag_name"`
	HTMLURL string `json:"html_url"`
}

func checkForUpdate() (newVersion, url string, hasUpdate bool) {
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get("https://api.github.com/repos/welcomra1n/session-manager-tui/releases/latest")
	if err != nil {
		return "", "", false
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", "", false
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", "", false
	}
	var release githubRelease
	if json.Unmarshal(body, &release) != nil {
		return "", "", false
	}
	tag := strings.TrimPrefix(release.TagName, "v")
	if tag != "" && tag != currentVersion {
		return tag, release.HTMLURL, true
	}
	return "", "", false
}

func selfUpdate() error {
	newVer, _, has := checkForUpdate()
	if !has {
		fmt.Println("이미 최신 버전입니다:", currentVersion)
		return nil
	}
	fmt.Printf("업데이트 발견: %s → %s\n", currentVersion, newVer)

	// Determine binary name for this platform
	goos := runtime.GOOS
	goarch := runtime.GOARCH
	binName := fmt.Sprintf("csm-%s-%s", goos, goarch)
	if goos == "windows" {
		binName += ".exe"
	}

	dlURL := fmt.Sprintf("https://github.com/welcomra1n/session-manager-tui/releases/download/v%s/%s", newVer, binName)
	fmt.Println("다운로드:", dlURL)

	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Get(dlURL)
	if err != nil {
		return fmt.Errorf("다운로드 실패: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("다운로드 실패: HTTP %d", resp.StatusCode)
	}

	// Get current executable path
	execPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("실행 파일 경로 확인 실패: %v", err)
	}
	// Resolve symlinks
	execPath, err = filepath.EvalSymlinks(execPath)
	if err != nil {
		return fmt.Errorf("심볼릭 링크 해석 실패: %v", err)
	}

	// Write to temp file first
	tmpFile := execPath + ".tmp"
	f, err := os.OpenFile(tmpFile, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
	if err != nil {
		return fmt.Errorf("임시 파일 생성 실패: %v", err)
	}
	if _, err := io.Copy(f, resp.Body); err != nil {
		f.Close()
		os.Remove(tmpFile)
		return fmt.Errorf("쓰기 실패: %v", err)
	}
	f.Close()

	// Replace old binary
	if err := os.Rename(tmpFile, execPath); err != nil {
		os.Remove(tmpFile)
		return fmt.Errorf("교체 실패: %v", err)
	}

	fmt.Printf("✅ 업데이트 완료: v%s\n", newVer)
	return nil
}

// ── Config file ─────────────────────────────────────────────────────────────

type Config struct {
	ExpiryDays      int    `json:"expiry_days"`
	RefreshInterval int    `json:"refresh_interval"`
	DefaultSort     string `json:"default_sort"`
	DefaultTerminal string `json:"default_terminal"`
	CompactMode     bool   `json:"compact_mode"`
}

func defaultConfig() Config {
	return Config{
		ExpiryDays:      30,
		RefreshInterval: 10,
		DefaultSort:     "date",
		DefaultTerminal: "auto",
		CompactMode:     false,
	}
}

func configPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude", "csm-config.json")
}

func loadConfig() Config {
	cfg := defaultConfig()
	data, err := os.ReadFile(configPath())
	if err != nil {
		return cfg
	}
	json.Unmarshal(data, &cfg)
	if cfg.ExpiryDays <= 0 {
		cfg.ExpiryDays = 30
	}
	if cfg.RefreshInterval <= 0 {
		cfg.RefreshInterval = 10
	}
	return cfg
}

func saveConfig(cfg Config) {
	data, _ := json.MarshalIndent(cfg, "", "  ")
	os.WriteFile(configPath(), data, 0644)
}

// ── Constants ───────────────────────────────────────────────────────────────

var sessionExpiryDays = 30

// ── Data types ──────────────────────────────────────────────────────────────

type Provider int

const (
	ProviderClaude Provider = iota
	ProviderCodex
)

func (p Provider) Icon() string {
	switch p {
	case ProviderCodex:
		return "\xf0\x9f\xa4\x96" // 🤖 (Codex)
	default:
		return "\xf0\x9f\xa7\xa0" // 🧠 (Claude)
	}
}

func (p Provider) Label() string {
	switch p {
	case ProviderCodex:
		return "Codex"
	default:
		return "Claude"
	}
}

type Session struct {
	ID           string
	ProjectDir   string
	ProjectName  string
	SessionFile  string
	ModTime      time.Time
	FileSize     int64
	MessageCount int
	UserMsgCount int
	AsstMsgCount int
	FirstUserMsg string
	LastUserMsg  string
	GitBranch    string
	CWD          string
	Messages     []Message
	Alias        string   // user-defined alias for the session
	Selected     bool     // multi-select checkbox state
	Provider     Provider // Claude or Codex
	Entrypoint   string   // cli, claude-desktop, web, etc.
	Active       bool     // session is currently open by another process
	Pinned       bool     // pinned in Codex desktop
}

type Message struct {
	Type    string
	Content string
}

type rawLine struct {
	Type       string          `json:"type"`
	Message    json.RawMessage `json:"message,omitempty"`
	GitBranch  string          `json:"gitBranch,omitempty"`
	CWD        string          `json:"cwd,omitempty"`
	Entrypoint string          `json:"entrypoint,omitempty"`
}

// ── Korean Dubeolsik → English key mapping ──────────────────────────────────

var korToEng = map[rune]rune{
	// Jamo (자모)
	'ㅂ': 'q', 'ㅈ': 'w', 'ㄷ': 'e', 'ㄱ': 'r', 'ㅅ': 't',
	'ㅛ': 'y', 'ㅕ': 'u', 'ㅑ': 'i', 'ㅐ': 'o', 'ㅔ': 'p',
	'ㅁ': 'a', 'ㄴ': 's', 'ㅇ': 'd', 'ㄹ': 'f', 'ㅎ': 'g',
	'ㅗ': 'h', 'ㅓ': 'j', 'ㅏ': 'k', 'ㅣ': 'l',
	'ㅋ': 'z', 'ㅌ': 'x', 'ㅊ': 'c', 'ㅍ': 'v', 'ㅠ': 'b',
	'ㅜ': 'n', 'ㅡ': 'm',
	// Shift variants
	'ㅃ': 'Q', 'ㅉ': 'W', 'ㄸ': 'E', 'ㄲ': 'R', 'ㅆ': 'T',
	'ㅒ': 'O', 'ㅖ': 'P',
}

// korSyllableToEng decomposes a Korean syllable to its initial consonant
// and maps it to the English key. This handles IME-composed characters.
func korSyllableToEng(r rune) (rune, bool) {
	// Direct jamo match
	if eng, ok := korToEng[r]; ok {
		return eng, true
	}
	// Decompose Korean syllable (가=0xAC00) to initial consonant
	if r >= 0xAC00 && r <= 0xD7A3 {
		// Korean syllable block: (initial * 21 + medial) * 28 + final
		idx := r - 0xAC00
		initial := idx / (21 * 28)
		// Initial consonants in order: ㄱㄲㄴㄷㄸㄹㅁㅂㅃㅅㅆㅇㅈㅉㅊㅋㅌㅍㅎ
		initials := []rune{'ㄱ', 'ㄲ', 'ㄴ', 'ㄷ', 'ㄸ', 'ㄹ', 'ㅁ', 'ㅂ', 'ㅃ', 'ㅅ', 'ㅆ', 'ㅇ', 'ㅈ', 'ㅉ', 'ㅊ', 'ㅋ', 'ㅌ', 'ㅍ', 'ㅎ'}
		if int(initial) < len(initials) {
			if eng, ok := korToEng[initials[initial]]; ok {
				return eng, true
			}
		}
	}
	return r, false
}

func toEngKey(r rune) rune {
	if eng, ok := korSyllableToEng(r); ok {
		return eng
	}
	return r
}

// isWide returns true if rune takes 2 cells in terminal (CJK, emoji, etc.)
func isWide(r rune) bool {
	return (r >= 0x1100 && r <= 0x115F) || // Hangul Jamo
		(r >= 0x2E80 && r <= 0x9FFF) || // CJK
		(r >= 0xAC00 && r <= 0xD7AF) || // Hangul Syllables
		(r >= 0xF900 && r <= 0xFAFF) || // CJK Compatibility
		(r >= 0xFE30 && r <= 0xFE6F) || // CJK Forms
		(r >= 0xFF01 && r <= 0xFF60) || // Fullwidth
		(r >= 0x1F000 && r <= 0x1FFFF) // Emoji
}

// visibleWidth counts display width (ignoring tview color tags, CJK=2)
func visibleWidth(s string) int {
	w := 0
	inTag := false
	for _, r := range s {
		if r == '[' {
			inTag = true
			continue
		}
		if inTag {
			if r == ']' {
				inTag = false
			}
			continue
		}
		if isWide(r) {
			w += 2
		} else {
			w++
		}
	}
	return w
}

// padRight pads a string to n visible cells, truncating if too long
func padRight(s string, n int) string {
	w := visibleWidth(s)
	if w > n {
		// Truncate
		cur := 0
		inTag := false
		var result strings.Builder
		for _, r := range s {
			if r == '[' {
				inTag = true
				result.WriteRune(r)
				continue
			}
			if inTag {
				result.WriteRune(r)
				if r == ']' {
					inTag = false
				}
				continue
			}
			rw := 1
			if isWide(r) {
				rw = 2
			}
			if cur+rw > n-1 {
				result.WriteRune('…')
				return result.String()
			}
			result.WriteRune(r)
			cur += rw
		}
		return result.String()
	}
	if w < n {
		return s + strings.Repeat(" ", n-w)
	}
	return s
}

// isSessionActive checks if a session is currently in use.
// 1) File modified within 2 minutes, OR
// 2) A claude/codex process is running with this session ID
func isSessionActive(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	if time.Since(info.ModTime()) < 2*time.Minute {
		return true
	}
	return false
}

// cachedActiveIDs caches process-based active session IDs (refreshed periodically)
// value: "local" (Ghostty/iTerm/Terminal), "ssh" (SSH session), "unknown"
var cachedActiveIDs map[string]string
var cachedActiveIDsTime time.Time

func refreshActiveIDs() map[string]string {
	if time.Since(cachedActiveIDsTime) < 5*time.Second && cachedActiveIDs != nil {
		return cachedActiveIDs
	}
	ids := make(map[string]string)

	detectEnv := func(line, sessionID string) string {
		lower := strings.ToLower(line)
		if strings.Contains(lower, "ssh") || strings.Contains(lower, "sshd") {
			return "ssh"
		}
		if strings.Contains(lower, "ghostty") || strings.Contains(lower, "iterm") ||
			strings.Contains(lower, "terminal") || strings.Contains(lower, "kitty") ||
			strings.Contains(lower, "wezterm") {
			return "local"
		}
		return "local"
	}

	// Check claude processes: claude --resume <ID>
	if out, err := exec.Command("pgrep", "-afl", "claude.*--resume").CombinedOutput(); err == nil {
		for _, line := range strings.Split(string(out), "\n") {
			if idx := strings.Index(line, "--resume"); idx >= 0 {
				rest := strings.TrimSpace(line[idx+len("--resume"):])
				fields := strings.Fields(rest)
				if len(fields) > 0 {
					ids[fields[0]] = detectEnv(line, fields[0])
				}
			}
		}
	}
	// Check codex processes: codex resume <ID>
	if out, err := exec.Command("pgrep", "-afl", "codex.*resume").CombinedOutput(); err == nil {
		for _, line := range strings.Split(string(out), "\n") {
			if idx := strings.Index(line, "resume"); idx >= 0 {
				rest := strings.TrimSpace(line[idx+len("resume"):])
				fields := strings.Fields(rest)
				if len(fields) > 0 {
					for _, f := range fields {
						if !strings.HasPrefix(f, "-") {
							ids[f] = detectEnv(line, f)
							break
						}
					}
				}
			}
		}
	}
	cachedActiveIDs = ids
	cachedActiveIDsTime = time.Now()
	return ids
}

func isSessionActiveByProcess(sessionID string) bool {
	ids := refreshActiveIDs()
	_, exists := ids[sessionID]
	return exists
}

func sessionActiveEnv(sessionID string) string {
	ids := refreshActiveIDs()
	if env, ok := ids[sessionID]; ok {
		return env
	}
	return ""
}

func entrypointIcon(ep string) string {
	switch ep {
	case "cli":
		return "CLI"
	case "claude-desktop":
		return "DSK"
	case "web", "claude-ai":
		return "WEB"
	default:
		if ep != "" {
			return ep
		}
		return "  "
	}
}

func entrypointLabel(ep string) string {
	switch ep {
	case "cli":
		return "CLI"
	case "claude-desktop":
		return "Desktop"
	case "web", "claude-ai":
		return "Web"
	default:
		if ep != "" {
			return ep
		}
		return ""
	}
}

type msgEnvelope struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

type contentBlock struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

// ── Path decoding ───────────────────────────────────────────────────────────

func decodePath(enc string) string {
	if enc == "" {
		return ""
	}
	if result := resolveEncoded("/", enc[1:]); result != "" {
		return result
	}
	return "/" + strings.ReplaceAll(enc[1:], "-", "/")
}

func resolveEncoded(base, remaining string) string {
	if remaining == "" {
		return base
	}
	parts := strings.Split(remaining, "-")
	for segLen := len(parts); segLen >= 1; segLen-- {
		segment := strings.Join(parts[:segLen], "-")
		candidate := filepath.Join(base, segment)
		info, err := os.Stat(candidate)
		if err != nil || !info.IsDir() {
			continue
		}
		rest := ""
		if segLen < len(parts) {
			rest = strings.Join(parts[segLen:], "-")
		}
		if rest == "" {
			return candidate
		}
		if result := resolveEncoded(candidate, rest); result != "" {
			return result
		}
	}
	return ""
}

// ── Text helpers ────────────────────────────────────────────────────────────

func lastSegment(p string) string {
	parts := strings.Split(p, "/")
	return parts[len(parts)-1]
}

func esc(s string) string { return strings.ReplaceAll(s, "[", "[[]") }

func fmtSize(b int64) string {
	switch {
	case b >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(b)/(1<<20))
	case b >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(b)/(1<<10))
	default:
		return fmt.Sprintf("%d B", b)
	}
}

func trunc(s string, n int) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if len(s) > n {
		return s[:n-3] + "..."
	}
	return s
}

func isMeaningfulMsg(s string) bool {
	if len(s) < 3 {
		return false
	}
	first := s
	if idx := strings.IndexAny(s, "\r\n"); idx >= 0 {
		first = strings.TrimSpace(s[:idx])
	}
	if first == "" {
		return false
	}
	if strings.HasPrefix(first, "Caveat:") {
		return false
	}
	if strings.HasPrefix(first, "/") {
		word := strings.Fields(first)[0]
		if !strings.Contains(word[1:], "/") {
			return false
		}
	}
	for _, prefix := range []string{"Set model to", "model", "Model set to"} {
		if first == prefix || strings.HasPrefix(first, prefix+" ") {
			return false
		}
	}
	return true
}

// ── JSONL parsing ───────────────────────────────────────────────────────────

var metaTags = []string{
	"<local-command-caveat>", "</local-command-caveat>",
	"<command-name>", "</command-name>",
	"<command-message>", "</command-message>",
	"<command-args>", "</command-args>",
	"<local-command-stdout>", "</local-command-stdout>",
	"<system-reminder>", "</system-reminder>",
}

func cleanMeta(s string) string {
	for _, tag := range metaTags {
		s = strings.ReplaceAll(s, tag, "")
	}
	return strings.TrimSpace(s)
}

func extractText(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return cleanMeta(s)
	}
	var blocks []contentBlock
	if json.Unmarshal(raw, &blocks) == nil {
		var parts []string
		for _, b := range blocks {
			if b.Type == "text" && b.Text != "" {
				parts = append(parts, cleanMeta(b.Text))
			}
		}
		return strings.Join(parts, "\n")
	}
	return ""
}

// ── Codex session loading ───────────────────────────────────────────────────

type codexIndexEntry struct {
	ID         string `json:"id"`
	ThreadName string `json:"thread_name"`
	UpdatedAt  string `json:"updated_at"`
	CWD        string `json:"cwd"`
}

type codexLine struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

type codexPayload struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

type codexContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

func extractCodexText(raw json.RawMessage) string {
	var blocks []codexContentBlock
	if json.Unmarshal(raw, &blocks) == nil {
		var parts []string
		for _, b := range blocks {
			if b.Type == "input_text" || b.Type == "output_text" {
				if t := strings.TrimSpace(b.Text); t != "" {
					parts = append(parts, t)
				}
			}
		}
		return strings.Join(parts, "\n")
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return strings.TrimSpace(s)
	}
	return ""
}

func findCodexSessionFile(id string) string {
	home, _ := os.UserHomeDir()
	base := filepath.Join(home, ".codex", "sessions")
	pattern := filepath.Join(base, "**", "rollout-*-"+id+".jsonl")
	matches, _ := filepath.Glob(pattern)
	if len(matches) > 0 {
		return matches[0]
	}
	// Walk year/month/day directories
	years, _ := os.ReadDir(base)
	for _, y := range years {
		if !y.IsDir() {
			continue
		}
		months, _ := os.ReadDir(filepath.Join(base, y.Name()))
		for _, m := range months {
			if !m.IsDir() {
				continue
			}
			days, _ := os.ReadDir(filepath.Join(base, y.Name(), m.Name()))
			for _, d := range days {
				if !d.IsDir() {
					continue
				}
				files, _ := os.ReadDir(filepath.Join(base, y.Name(), m.Name(), d.Name()))
				for _, f := range files {
					if strings.Contains(f.Name(), id) {
						return filepath.Join(base, y.Name(), m.Name(), d.Name(), f.Name())
					}
				}
			}
		}
	}
	return ""
}

func loadCodexSession(entry codexIndexEntry) *Session {
	sessionFile := findCodexSessionFile(entry.ID)
	if sessionFile == "" {
		return nil
	}

	info, err := os.Stat(sessionFile)
	if err != nil {
		return nil
	}

	cwd := entry.CWD
	home, _ := os.UserHomeDir()
	projectName := lastSegment(cwd)
	if projectName == "" || strings.HasPrefix(projectName, "20") || cwd == home {
		projectName = "미분류"
	}

	sess := &Session{
		ID:          entry.ID,
		ProjectDir:  cwd,
		ProjectName: projectName,
		SessionFile: sessionFile,
		ModTime:     info.ModTime(),
		FileSize:    info.Size(),
		CWD:         cwd,
		Provider:    ProviderCodex,
	}

	// Use file ModTime (more accurate than index updated_at, which may be stale)

	f, err := os.Open(sessionFile)
	if err != nil {
		return nil
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1<<20), 10<<20)

	for sc.Scan() {
		var line codexLine
		if json.Unmarshal(sc.Bytes(), &line) != nil {
			continue
		}
		// Read CWD from session_meta
		if line.Type == "session_meta" && line.Payload != nil {
			var meta struct {
				CWD string `json:"cwd"`
			}
			if json.Unmarshal(line.Payload, &meta) == nil && meta.CWD != "" {
				sess.CWD = meta.CWD
				sess.ProjectDir = meta.CWD
				home, _ := os.UserHomeDir()
				name := lastSegment(meta.CWD)
				if strings.HasPrefix(name, "20") || meta.CWD == home {
					name = "미분류"
				}
				sess.ProjectName = name
			}
			continue
		}
		if line.Type != "response_item" || line.Payload == nil {
			continue
		}
		var payload codexPayload
		if json.Unmarshal(line.Payload, &payload) != nil {
			continue
		}
		if payload.Role != "user" && payload.Role != "assistant" {
			continue
		}
		text := extractCodexText(payload.Content)
		if text == "" {
			continue
		}
		// Skip very long system-like messages
		if len(text) > 2000 {
			text = text[:2000] + "..."
		}

		msgType := payload.Role
		sess.Messages = append(sess.Messages, Message{Type: msgType, Content: text})
		sess.MessageCount++

		if msgType == "user" {
			sess.UserMsgCount++
			c := strings.TrimSpace(text)
			if len(c) > 3 && !strings.HasPrefix(c, "#") && !strings.HasPrefix(c, "<") {
				if sess.FirstUserMsg == "" {
					sess.FirstUserMsg = c
				}
				sess.LastUserMsg = c
			}
		} else {
			sess.AsstMsgCount++
		}
	}

	// Use thread_name as session title (Codex desktop shows this as session name)
	if entry.ThreadName != "" {
		sess.Alias = entry.ThreadName
		if len(sess.Alias) > 40 {
			sess.Alias = sess.Alias[:37] + "..."
		}
	}
	if sess.FirstUserMsg == "" && entry.ThreadName != "" {
		sess.FirstUserMsg = entry.ThreadName
	}

	if sess.MessageCount == 0 {
		return nil
	}
	return sess
}

func discoverCodexSessions() []*Session {
	home, _ := os.UserHomeDir()
	indexPath := filepath.Join(home, ".codex", "session_index.jsonl")

	f, err := os.Open(indexPath)
	if err != nil {
		return nil
	}
	defer f.Close()

	// Deduplicate by ID — keep latest entry only
	seen := make(map[string]codexIndexEntry)
	var order []string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1<<20), 10<<20)

	for sc.Scan() {
		var entry codexIndexEntry
		if json.Unmarshal(sc.Bytes(), &entry) != nil {
			continue
		}
		if _, exists := seen[entry.ID]; !exists {
			order = append(order, entry.ID)
		}
		seen[entry.ID] = entry // last write wins (latest update)
	}

	var out []*Session
	for _, id := range order {
		entry := seen[id]
		if s := loadCodexSession(entry); s != nil {
			out = append(out, s)
		}
	}
	return out
}

// ── Session deletion ────────────────────────────────────────────────────────

func trashDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude", "session-trash")
}

func deleteSession(s *Session) error {
	// Move to trash instead of deleting
	trash := trashDir()
	os.MkdirAll(trash, 0755)

	destName := filepath.Base(s.SessionFile)
	destPath := filepath.Join(trash, destName)
	if err := os.Rename(s.SessionFile, destPath); err != nil {
		// If rename fails (cross-device), copy then delete
		data, readErr := os.ReadFile(s.SessionFile)
		if readErr != nil {
			return readErr
		}
		if writeErr := os.WriteFile(destPath, data, 0644); writeErr != nil {
			return writeErr
		}
		os.Remove(s.SessionFile)
	}

	// Save metadata for restore
	meta := map[string]string{
		"originalPath": s.SessionFile,
		"provider":     fmt.Sprintf("%d", s.Provider),
		"id":           s.ID,
		"deletedAt":    time.Now().Format(time.RFC3339),
	}
	metaData, _ := json.Marshal(meta)
	os.WriteFile(destPath+".meta", metaData, 0644)

	// For Codex sessions, also remove from session_index.jsonl
	if s.Provider == ProviderCodex {
		removeFromCodexIndex(s.ID)
	}
	return nil
}

func cleanOldTrash() {
	items := listTrash()
	for _, item := range items {
		if t, err := time.Parse(time.RFC3339, item["deletedAt"]); err == nil {
			if time.Since(t) > 30*24*time.Hour {
				permanentDeleteTrash(item)
			}
		}
	}
}

func listTrash() []map[string]string {
	trash := trashDir()
	entries, err := os.ReadDir(trash)
	if err != nil {
		return nil
	}
	var items []map[string]string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".meta") {
			data, err := os.ReadFile(filepath.Join(trash, e.Name()))
			if err != nil {
				continue
			}
			var meta map[string]string
			if json.Unmarshal(data, &meta) == nil {
				meta["trashFile"] = filepath.Join(trash, strings.TrimSuffix(e.Name(), ".meta"))
				items = append(items, meta)
			}
		}
	}
	return items
}

func restoreFromTrash(item map[string]string) error {
	trashFile := item["trashFile"]
	origPath := item["originalPath"]
	// Ensure parent dir exists
	os.MkdirAll(filepath.Dir(origPath), 0755)
	if err := os.Rename(trashFile, origPath); err != nil {
		return err
	}
	os.Remove(trashFile + ".meta")
	return nil
}

func permanentDeleteTrash(item map[string]string) error {
	os.Remove(item["trashFile"])
	os.Remove(item["trashFile"] + ".meta")
	return nil
}

func removeFromCodexIndex(id string) {
	home, _ := os.UserHomeDir()
	indexPath := filepath.Join(home, ".codex", "session_index.jsonl")

	data, err := os.ReadFile(indexPath)
	if err != nil {
		return
	}

	var kept []string
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line == "" {
			continue
		}
		var entry codexIndexEntry
		if json.Unmarshal([]byte(line), &entry) == nil && entry.ID == id {
			continue // skip this entry
		}
		kept = append(kept, line)
	}

	os.WriteFile(indexPath, []byte(strings.Join(kept, "\n")+"\n"), 0644)
}

// ── Codex pin detection ─────────────────────────────────────────────────────

func saveCodexPins(pins map[string]bool) {
	home, _ := os.UserHomeDir()
	path := filepath.Join(home, ".codex", ".codex-global-state.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var state map[string]json.RawMessage
	if json.Unmarshal(data, &state) != nil {
		return
	}
	var ids []string
	for id := range pins {
		ids = append(ids, id)
	}
	raw, _ := json.Marshal(ids)
	state["pinned-thread-ids"] = raw
	out, _ := json.MarshalIndent(state, "", "  ")
	os.WriteFile(path, out, 0644)
}

func loadCodexPins() map[string]bool {
	home, _ := os.UserHomeDir()
	data, err := os.ReadFile(filepath.Join(home, ".codex", ".codex-global-state.json"))
	if err != nil {
		return nil
	}
	var state map[string]json.RawMessage
	if json.Unmarshal(data, &state) != nil {
		return nil
	}
	raw, ok := state["pinned-thread-ids"]
	if !ok {
		return nil
	}
	var ids []string
	if json.Unmarshal(raw, &ids) != nil {
		return nil
	}
	pins := make(map[string]bool)
	for _, id := range ids {
		pins[id] = true
	}
	return pins
}

// ── Pin persistence (local, works for both Claude and Codex) ────────────────

func pinFilePath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude", "session-pins.json")
}

func unpinFilePath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude", "session-unpins.json")
}

func loadUnpins() map[string]bool {
	unpins := make(map[string]bool)
	data, err := os.ReadFile(unpinFilePath())
	if err == nil {
		var ids []string
		if json.Unmarshal(data, &ids) == nil {
			for _, id := range ids {
				unpins[id] = true
			}
		}
	}
	return unpins
}

func saveUnpins(unpins map[string]bool) {
	var ids []string
	for id := range unpins {
		ids = append(ids, id)
	}
	data, _ := json.MarshalIndent(ids, "", "  ")
	os.WriteFile(unpinFilePath(), data, 0644)
}

func loadPins() map[string]bool {
	// Load our local pins
	pins := make(map[string]bool)
	data, err := os.ReadFile(pinFilePath())
	if err == nil {
		var ids []string
		if json.Unmarshal(data, &ids) == nil {
			for _, id := range ids {
				pins[id] = true
			}
		}
	}
	// Also load Codex desktop pins (minus locally unpinned)
	unpins := loadUnpins()
	codexPins := loadCodexPins()
	for id := range codexPins {
		if !unpins[id] {
			pins[id] = true
		}
	}
	return pins
}

func savePins(pins map[string]bool) error {
	var ids []string
	for id := range pins {
		ids = append(ids, id)
	}
	data, err := json.MarshalIndent(ids, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(pinFilePath(), data, 0644)
}

// ── Alias persistence ───────────────────────────────────────────────────────

// ── Project alias persistence ────────────────────────────────────────────────

func projectAliasFilePath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude", "project-aliases.json")
}

func loadProjectAliases() map[string]string {
	data, err := os.ReadFile(projectAliasFilePath())
	if err != nil {
		return make(map[string]string)
	}
	var aliases map[string]string
	if json.Unmarshal(data, &aliases) != nil {
		return make(map[string]string)
	}
	return aliases
}

func saveProjectAliases(aliases map[string]string) {
	data, _ := json.MarshalIndent(aliases, "", "  ")
	os.WriteFile(projectAliasFilePath(), data, 0644)
}

func aliasFilePath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude", "session-aliases.json")
}

func loadAliases() map[string]string {
	data, err := os.ReadFile(aliasFilePath())
	if err != nil {
		return make(map[string]string)
	}
	var aliases map[string]string
	if json.Unmarshal(data, &aliases) != nil {
		return make(map[string]string)
	}
	return aliases
}

func saveAliases(aliases map[string]string) error {
	data, err := json.MarshalIndent(aliases, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(aliasFilePath(), data, 0644)
}

// ── Expiry helpers ──────────────────────────────────────────────────────────

func daysUntilExpiry(s *Session) int {
	expiry := s.ModTime.AddDate(0, 0, sessionExpiryDays)
	return int(time.Until(expiry).Hours() / 24)
}

func expiryIcon(s *Session) string {
	d := daysUntilExpiry(s)
	switch {
	case d < 0:
		return "\xf0\x9f\x92\x80" // 💀 expired
	case d <= 3:
		return "\xf0\x9f\x94\xb4" // 🔴 expiring very soon
	case d <= 7:
		return "\xf0\x9f\x9f\xa1" // 🟡 expiring soon
	default:
		return "\xf0\x9f\x9f\xa2" // 🟢 healthy
	}
}

func expiryLabel(s *Session) string {
	d := daysUntilExpiry(s)
	if d < 0 {
		return fmt.Sprintf("D+%d", -d)
	}
	if d == 0 {
		return "D-Day"
	}
	return fmt.Sprintf("D-%d", d)
}

// ── Session loading ─────────────────────────────────────────────────────────

func loadSession(path string) *Session {
	info, err := os.Stat(path)
	if err != nil {
		return nil
	}
	dir := filepath.Base(filepath.Dir(path))
	ppath := decodePath(dir)
	home, _ := os.UserHomeDir()
	pname := lastSegment(ppath)
	if ppath == home {
		pname = "미분류"
	}
	sess := &Session{
		ID:          strings.TrimSuffix(filepath.Base(path), ".jsonl"),
		ProjectDir:  ppath,
		ProjectName: pname,
		SessionFile: path,
		ModTime:     info.ModTime(),
		FileSize:    info.Size(),
	}

	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1<<20), 10<<20)

	for sc.Scan() {
		var raw rawLine
		if json.Unmarshal(sc.Bytes(), &raw) != nil {
			continue
		}
		// Read custom title from /rename command
		if raw.Type == "custom-title" {
			var ct struct {
				CustomTitle string `json:"customTitle"`
			}
			if json.Unmarshal(sc.Bytes(), &ct) == nil && ct.CustomTitle != "" {
				sess.Alias = ct.CustomTitle
			}
			continue
		}
		if raw.Type != "user" && raw.Type != "assistant" {
			continue
		}
		var env msgEnvelope
		if json.Unmarshal(raw.Message, &env) != nil {
			continue
		}
		text := extractText(env.Content)
		if text == "" {
			continue
		}

		sess.Messages = append(sess.Messages, Message{Type: raw.Type, Content: text})
		sess.MessageCount++

		if sess.GitBranch == "" && raw.GitBranch != "" {
			sess.GitBranch = raw.GitBranch
		}
		if sess.CWD == "" && raw.CWD != "" {
			sess.CWD = raw.CWD
		}
		if sess.Entrypoint == "" && raw.Entrypoint != "" {
			sess.Entrypoint = raw.Entrypoint
		}

		if raw.Type == "user" {
			sess.UserMsgCount++
			c := strings.TrimSpace(text)
			if isMeaningfulMsg(c) {
				if sess.FirstUserMsg == "" {
					sess.FirstUserMsg = c
				}
				sess.LastUserMsg = c
			}
		} else {
			sess.AsstMsgCount++
		}
	}

	if sess.MessageCount == 0 || (sess.UserMsgCount <= 1 && sess.AsstMsgCount <= 1) {
		return nil
	}
	return sess
}

func discoverSessions() []*Session {
	home, _ := os.UserHomeDir()
	base := filepath.Join(home, ".claude", "projects")
	entries, err := os.ReadDir(base)
	if err != nil {
		return nil
	}
	aliases := loadAliases()
	var out []*Session
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		dir := filepath.Join(base, e.Name())
		files, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, f := range files {
			if strings.HasSuffix(f.Name(), ".jsonl") {
				if s := loadSession(filepath.Join(dir, f.Name())); s != nil {
					if alias, ok := aliases[s.ID]; ok {
						s.Alias = alias
					}
					out = append(out, s)
				}
			}
		}
	}
	// Merge Codex sessions
	codexSessions := discoverCodexSessions()
	for _, cs := range codexSessions {
		if alias, ok := aliases[cs.ID]; ok {
			cs.Alias = alias
		}
		out = append(out, cs)
	}

	// Apply pins (both local and Codex desktop)
	pins := loadPins()
	for _, s := range out {
		if pins[s.ID] {
			s.Pinned = true
		}
	}

	// Detect active sessions (file mod time OR running process)
	for _, s := range out {
		s.Active = isSessionActive(s.SessionFile) || isSessionActiveByProcess(s.ID)
	}

	sort.Slice(out, func(i, j int) bool { return out[i].ModTime.After(out[j].ModTime) })
	return out
}

// ── Terminal backend detection & integration ────────────────────────────────

type termBackend int

const (
	backendITerm2 termBackend = iota
	backendTerminalApp
	backendTmux
	backendKitty
	backendWezTerm
	backendGhostty
	backendSSH
	backendFallback
)

func (b termBackend) String() string {
	names := [...]string{"iTerm2", "Terminal.app", "tmux", "Kitty", "WezTerm", "Ghostty", "SSH", "fallback"}
	if int(b) < len(names) {
		return names[b]
	}
	return "unknown"
}

var activeBackend termBackend

func detectBackend() termBackend {
	if os.Getenv("TMUX") != "" {
		return backendTmux
	}
	switch os.Getenv("TERM_PROGRAM") {
	case "iTerm.app":
		return backendITerm2
	case "Apple_Terminal":
		return backendTerminalApp
	case "WezTerm":
		return backendWezTerm
	case "ghostty":
		return backendGhostty
	}
	if os.Getenv("KITTY_PID") != "" {
		return backendKitty
	}
	// SSH detection — use tmux to open sessions in new window
	if os.Getenv("SSH_CONNECTION") != "" || os.Getenv("SSH_CLIENT") != "" {
		return backendSSH
	}
	return backendFallback
}

func escapeShell(s string) string {
	return strings.ReplaceAll(s, "'", "'\\''")
}

func escapeAppleScript(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	return strings.ReplaceAll(s, "\"", "\\\"")
}

func runAppleScript(script string) error {
	return exec.Command("osascript", "-e", script).Run()
}

func openInTerminal(command, dir string, inTab bool, app *tview.Application) error {
	switch activeBackend {
	case backendITerm2:
		return iterm2Open(command, dir, inTab)
	case backendTerminalApp:
		return terminalAppOpen(command, dir, inTab)
	case backendTmux:
		return tmuxOpen(command, dir, inTab)
	case backendKitty:
		return kittyOpen(command, dir, inTab)
	case backendWezTerm:
		return weztermOpen(command, dir, inTab)
	case backendGhostty:
		return ghosttyOpen(command, dir, inTab)
	case backendSSH:
		return sshOpen(command, dir, inTab, app)
	default:
		var runErr error
		app.Suspend(func() {
			cmd := exec.Command("sh", "-c", fmt.Sprintf("cd '%s' && %s", escapeShell(dir), command))
			cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
			runErr = cmd.Run()
		})
		return runErr
	}
}

func iterm2Open(command, dir string, inTab bool) error {
	cmd := escapeAppleScript(command)
	d := escapeAppleScript(dir)
	if inTab {
		return runAppleScript(fmt.Sprintf(`tell application "iTerm2"
	tell current window
		set newTab to (create tab with default profile)
		tell current session of newTab
			write text "cd \"%s\" && %s"
		end tell
	end tell
end tell`, d, cmd))
	}
	return runAppleScript(fmt.Sprintf(`tell application "iTerm2"
	tell current session of current window
		set newSession to (split vertically with default profile)
		tell newSession
			write text "cd \"%s\" && %s"
		end tell
	end tell
end tell`, d, cmd))
}

func terminalAppOpen(command, dir string, inTab bool) error {
	cmd := escapeAppleScript(command)
	d := escapeAppleScript(dir)
	if inTab {
		return runAppleScript(fmt.Sprintf(`tell application "System Events"
	tell process "Terminal"
		keystroke "t" using command down
	end tell
end tell
delay 0.3
tell application "Terminal"
	do script "cd \"%s\" && %s" in front window
end tell`, d, cmd))
	}
	return runAppleScript(fmt.Sprintf(`tell application "Terminal"
	activate
	do script "cd \"%s\" && %s"
end tell`, d, cmd))
}

func tmuxOpen(command, dir string, inTab bool) error {
	fullCmd := fmt.Sprintf("cd '%s' && %s", escapeShell(dir), command)
	if inTab {
		return exec.Command("tmux", "new-window", "-c", dir, fullCmd).Run()
	}
	return exec.Command("tmux", "split-window", "-h", "-c", dir, fullCmd).Run()
}

func kittyOpen(command, dir string, inTab bool) error {
	fullCmd := fmt.Sprintf("cd '%s' && %s", escapeShell(dir), command)
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/sh"
	}
	launchType := "window"
	if inTab {
		launchType = "tab"
	}
	return exec.Command("kitty", "@", "launch", "--type="+launchType, "--cwd="+dir, shell, "-c", fullCmd).Run()
}

func weztermOpen(command, dir string, inTab bool) error {
	fullCmd := fmt.Sprintf("cd '%s' && %s", escapeShell(dir), command)
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/sh"
	}
	if inTab {
		return exec.Command("wezterm", "cli", "spawn", "--cwd", dir, "--", shell, "-c", fullCmd).Run()
	}
	return exec.Command("wezterm", "cli", "split-pane", "--right", "--cwd", dir, "--", shell, "-c", fullCmd).Run()
}

func ghosttyOpen(command, dir string, inTab bool) error {
	fullCmd := fmt.Sprintf("cd '%s' && exec %s", escapeShell(dir), command)
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/sh"
	}
	return exec.Command("open", "-na", "Ghostty", "--args", "-e", shell, "-c", fullCmd).Run()
}

func sshOpen(command, dir string, inTab bool, app *tview.Application) error {
	fullCmd := fmt.Sprintf("cd '%s' && %s", escapeShell(dir), command)
	// If tmux is available, use it to open a new window (csm stays running)
	if _, err := exec.LookPath("tmux"); err == nil {
		// Check if we're inside tmux (user started tmux manually)
		if os.Getenv("TMUX") != "" {
			return tmuxOpen(command, dir, inTab)
		}
		// Not in tmux — create a new detached tmux session and attach
		sessionName := fmt.Sprintf("csm-%d", time.Now().UnixNano()%10000)
		if err := exec.Command("tmux", "new-session", "-d", "-s", sessionName, "-c", dir, fullCmd).Run(); err != nil {
			return err
		}
		// Suspend TUI, attach to tmux session, resume TUI when detached/exited
		var runErr error
		app.Suspend(func() {
			cmd := exec.Command("tmux", "attach-session", "-t", sessionName)
			cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
			runErr = cmd.Run()
		})
		return runErr
	}
	// No tmux — fallback to suspend
	var runErr error
	app.Suspend(func() {
		cmd := exec.Command("sh", "-c", fullCmd)
		cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
		runErr = cmd.Run()
	})
	return runErr
}

// ── AI Summary ──────────────────────────────────────────────────────────────

func buildConversationDigest(s *Session, budget int) string {
	var b strings.Builder
	for _, msg := range s.Messages {
		content := msg.Content
		if len(content) > 300 {
			content = content[:300] + "..."
		}
		line := fmt.Sprintf("[%s]: %s\n", msg.Type, content)
		if b.Len()+len(line) > budget {
			break
		}
		b.WriteString(line)
	}
	return b.String()
}

func generateSummary(s *Session) (string, error) {
	digest := buildConversationDigest(s, 4000)
	prompt := fmt.Sprintf(
		"Summarize this Claude Code session in 3-5 bullet points. "+
			"Focus on: what the user was trying to accomplish, key decisions made, and current status. Be concise.\n\n"+
			"Project: %s\nPath: %s\n\nConversation:\n%s",
		s.ProjectName, s.ProjectDir, digest,
	)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "claude", "-p", "--model", "haiku", "--bare", "--no-session-persistence")
	cmd.Stdin = strings.NewReader(prompt)
	out, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		return "", fmt.Errorf("timed out after 30s")
	}
	if err != nil {
		return "", fmt.Errorf("%v: %s", err, string(out))
	}
	return strings.TrimSpace(string(out)), nil
}

// ── Helper: get session from tree node ──────────────────────────────────────

func nodeSession(node *tview.TreeNode) *Session {
	if node == nil || node.GetReference() == nil {
		return nil
	}
	s, ok := node.GetReference().(*Session)
	if !ok {
		return nil
	}
	return s
}

func nodeRefStr(node *tview.TreeNode) string {
	if node == nil || node.GetReference() == nil {
		return ""
	}
	s, ok := node.GetReference().(string)
	if !ok {
		return ""
	}
	return s
}

// ── Session node text formatting ────────────────────────────────────────────

func highlightText(text, query string) string {
	if query == "" {
		return text
	}
	lower := strings.ToLower(text)
	lowerQ := strings.ToLower(query)
	idx := strings.Index(lower, lowerQ)
	if idx < 0 {
		return text
	}
	return text[:idx] + "[yellow]" + text[idx:idx+len(query)] + "[-][white]" + text[idx+len(query):]
}

func activeIconFor(s *Session) string {
	if !s.Active {
		return " "
	}
	env := sessionActiveEnv(s.ID)
	switch env {
	case "ssh":
		return "[lime]▶[-][#999999]R[-]"
	default:
		return "[lime]▶[-] "
	}
}

func sessionNodeTextCompact(s *Session, searchQuery ...string) string {
	activeIcon := activeIconFor(s)
	title := s.Alias
	if title == "" {
		title = trunc(s.FirstUserMsg, 25)
	}
	if title == "" {
		title = fmt.Sprintf("%d개", s.MessageCount)
	}
	displayTitle := title
	if len(searchQuery) > 0 && searchQuery[0] != "" {
		displayTitle = highlightText(title, searchQuery[0])
	}
	col1 := padRight(activeIcon, 2)
	col2 := padRight(fmt.Sprintf("[white]%s[-]", esc(displayTitle)), 22)
	col3 := padRight(fmt.Sprintf("[#999999]%s[-]", expiryLabel(s)), 5)
	return fmt.Sprintf("%s%s %s", col1, col2, col3)
}

func sessionNodeText(s *Session, searchQuery ...string) string {
	check := " "
	if s.Selected {
		check = "\xe2\x9c\x93" // ✓
	}
	title := s.Alias
	if title == "" {
		title = trunc(s.FirstUserMsg, 40)
	}
	if title == "" {
		title = fmt.Sprintf("%d개 메시지", s.MessageCount)
	}
	epIco := entrypointIcon(s.Entrypoint)
	if s.Provider == ProviderCodex {
		epIco = "DSK"
	}
	dateColor := "[#999999]"
	if daysUntilExpiry(s) < 0 {
		dateColor = "[#FF4444]"
	} else if time.Since(s.ModTime) < 2*time.Minute {
		dateColor = "[#00BFFF]"
	} else if time.Since(s.ModTime) < 7*24*time.Hour {
		dateColor = "[#00ff00]"
	}
	activeIcon := activeIconFor(s)

	col1 := padRight(fmt.Sprintf("%s%s", check, activeIcon), 4)
	col2 := padRight(fmt.Sprintf("%s%s[-]", dateColor, s.ModTime.Format("01/02 15:04")), 12)
	displayTitle := title
	if len(searchQuery) > 0 && searchQuery[0] != "" {
		displayTitle = highlightText(title, searchQuery[0])
	}
	col3 := padRight(fmt.Sprintf("[white]%s[-]", esc(displayTitle)), 30)
	col4 := padRight(fmt.Sprintf("[#999999]%s[-]", epIco), 4)
	col5 := padRight(fmt.Sprintf("[#999999]%s[-]", expiryLabel(s)), 6)
	return fmt.Sprintf("%s%s  %s  %s  %s", col1, col2, col3, col4, col5)
}

// ── Main ────────────────────────────────────────────────────────────────────

func main() {
	// CLI flags
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "--version", "-v":
			fmt.Printf("csm v%s (%s/%s)\n", currentVersion, runtime.GOOS, runtime.GOARCH)
			return
		case "--update", "-u":
			if err := selfUpdate(); err != nil {
				fmt.Fprintf(os.Stderr, "업데이트 실패: %v\n", err)
				os.Exit(1)
			}
			return
		case "--config":
			cfg := loadConfig()
			data, _ := json.MarshalIndent(cfg, "", "  ")
			fmt.Println("설정 파일:", configPath())
			fmt.Println(string(data))
			return
		case "--help", "-h":
			fmt.Println("csm — Claude Code + Codex 세션 매니저")
			fmt.Printf("버전: v%s\n\n", currentVersion)
			fmt.Println("사용법:")
			fmt.Println("  csm              TUI 실행")
			fmt.Println("  csm --version    버전 표시")
			fmt.Println("  csm --update     최신 버전으로 업데이트")
			fmt.Println("  csm --config     설정 파일 보기")
			fmt.Println("  csm --help       도움말")
			return
		}
	}

	// Load config
	cfg := loadConfig()
	sessionExpiryDays = cfg.ExpiryDays

	cleanOldTrash()
	fmt.Print("세션 불러오는 중...")
	sessions := discoverSessions()
	fmt.Print("\r\033[2K")

	activeBackend = detectBackend()
	app := tview.NewApplication()
	tview.Styles.PrimitiveBackgroundColor = tcell.NewRGBColor(0, 0, 0)
	summaryCache := make(map[string]string)
	aliases := loadAliases()
	localPins := loadPins()
	localUnpins := loadUnpins()
	projectAliases := loadProjectAliases()
	compactMode := cfg.CompactMode
	var updateInfo string // set by background check

	// ── Widgets ──

	selectedStyle := tcell.StyleDefault.Background(tcell.ColorDarkSlateGray).Foreground(tcell.ColorWhite)
	tree := tview.NewTreeView()
	tree.SetBorder(true).
		SetTitle(" 세션 목록 ").
		SetTitleAlign(tview.AlignLeft).
		SetBorderColor(tcell.ColorGreen)
	tree.SetGraphics(false)
	tree.SetTopLevel(1) // hide root

	infoView := tview.NewTextView().
		SetDynamicColors(true).
		SetWordWrap(true).
		SetScrollable(true)
	infoView.SetBorder(true).
		SetTitle(" 세션 정보 ").
		SetTitleAlign(tview.AlignLeft).
		SetBorderColor(tcell.ColorDodgerBlue)

	convView := tview.NewTextView().
		SetDynamicColors(true).
		SetWordWrap(true).
		SetScrollable(true)
	convView.SetBorder(true).
		SetTitle(" 대화 미리보기 ").
		SetTitleAlign(tview.AlignLeft).
		SetBorderColor(tcell.ColorDodgerBlue)

	searchInput := tview.NewInputField().
		SetLabel(" / ").
		SetLabelColor(tcell.ColorYellow).
		SetFieldBackgroundColor(tcell.ColorDefault).
		SetFieldTextColor(tcell.ColorWhite)

	helpBar := tview.NewTextView().
		SetDynamicColors(true).
		SetTextAlign(tview.AlignCenter)
	helpBar.SetText("[yellow]1-9[-] 빠른열기 | [yellow]Enter[-] 열기 | [yellow]p[-] 미리보기 | [yellow]t[-] 고정 | [yellow]m[-] 이름변경 | [yellow]d[-] 삭제 | [yellow]e[-] 내보내기\n[yellow]Space[-] 선택 | [yellow]D[-] 일괄삭제 | [yellow]E[-] 일괄내보내기 | [yellow]c[-] 컴팩트 | [yellow]x[-] 휴지통 | [yellow]/[-] 검색 | [yellow]?[-] 도움말 | [yellow]Esc[-] 종료")

	statusBar := tview.NewTextView().
		SetDynamicColors(true).
		SetTextAlign(tview.AlignCenter)

	// ── Layout ──

	leftPane := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(tree, 0, 1, true)

	rightPane := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(infoView, 0, 2, false).
		AddItem(convView, 0, 3, false)

	mainBody := tview.NewFlex().SetDirection(tview.FlexColumn).
		AddItem(leftPane, 0, 1, true)

	mainLayout := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(mainBody, 0, 1, true).
		AddItem(helpBar, 2, 0, false).
		AddItem(statusBar, 1, 0, false)

	previewOpen := false
	var togglePreview func()
	currentFilter := ""

	// Sort modes
	type SortMode int
	const (
		SortByDate SortMode = iota
		SortByName
		SortByExpiry
	)
	sortMode := SortByDate
	switch cfg.DefaultSort {
	case "name":
		sortMode = SortByName
	case "expiry":
		sortMode = SortByExpiry
	}
	sortLabels := map[SortMode]string{
		SortByDate:   "날짜순",
		SortByName:   "이름순",
		SortByExpiry: "만료순",
	}
	sortSessions := func() {
		switch sortMode {
		case SortByDate:
			sort.Slice(sessions, func(i, j int) bool { return sessions[i].ModTime.After(sessions[j].ModTime) })
		case SortByName:
			sort.Slice(sessions, func(i, j int) bool {
				ai, aj := sessions[i].Alias, sessions[j].Alias
				if ai == "" {
					ai = sessions[i].FirstUserMsg
				}
				if aj == "" {
					aj = sessions[j].FirstUserMsg
				}
				return strings.ToLower(ai) < strings.ToLower(aj)
			})
		case SortByExpiry:
			sort.Slice(sessions, func(i, j int) bool { return daysUntilExpiry(sessions[i]) < daysUntilExpiry(sessions[j]) })
		}
	}

	// ── Session display ──

	showSessionInfo := func(s *Session) {
		if s == nil {
			return
		}

		var b strings.Builder
		epInfo := entrypointLabel(s.Entrypoint)
		if s.Provider == ProviderCodex {
			epInfo = "Desktop"
		}
		if epInfo != "" {
			fmt.Fprintf(&b, "[yellow]제공자:[-]    %s %s (%s)\n", s.Provider.Icon(), s.Provider.Label(), epInfo)
		} else {
			fmt.Fprintf(&b, "[yellow]제공자:[-]    %s %s\n", s.Provider.Icon(), s.Provider.Label())
		}
		if s.Active {
			env := sessionActiveEnv(s.ID)
			envLabel := "로컬"
			if env == "ssh" {
				envLabel = "원격 (SSH)"
			}
			fmt.Fprintf(&b, "[yellow]상태:[-]        [lime]▶ 활성[-] (%s)\n", envLabel)
		}
		if s.Pinned {
			fmt.Fprintf(&b, "[yellow]고정:[-]        \xf0\x9f\x93\x8c 고정됨\n")
		}
		fmt.Fprintf(&b, "[yellow]프로젝트:[-]     %s\n", esc(s.ProjectName))
		if s.Alias != "" {
			fmt.Fprintf(&b, "[yellow]별칭:[-]       [aqua]%s[-]\n", esc(s.Alias))
		}
		fmt.Fprintf(&b, "[yellow]경로:[-]        %s\n", esc(s.ProjectDir))
		fmt.Fprintf(&b, "[yellow]세션:[-]     [green]%s[-]\n", s.ID)
		fmt.Fprintf(&b, "[yellow]수정일:[-]    %s\n", s.ModTime.Format("2006-01-02 15:04:05"))
		fmt.Fprintf(&b, "[yellow]만료:[-]      %s %s\n", expiryIcon(s), expiryLabel(s))
		fmt.Fprintf(&b, "[yellow]크기:[-]        %s\n", fmtSize(s.FileSize))
		fmt.Fprintf(&b, "[yellow]메시지:[-]    %d (%d 사용자, %d 어시스턴트)\n", s.MessageCount, s.UserMsgCount, s.AsstMsgCount)
		if s.GitBranch != "" {
			fmt.Fprintf(&b, "[yellow]브랜치:[-]  %s\n", esc(s.GitBranch))
		}
		if s.CWD != "" {
			fmt.Fprintf(&b, "[yellow]작업경로:[-]         %s\n", esc(s.CWD))
		}
		if s.FirstUserMsg != "" {
			fmt.Fprintf(&b, "\n[yellow]첫 메시지:[-]\n  %s\n", esc(trunc(s.FirstUserMsg, 200)))
		}
		if s.LastUserMsg != "" && s.LastUserMsg != s.FirstUserMsg {
			fmt.Fprintf(&b, "\n[yellow]마지막 메시지:[-]\n  %s\n", esc(trunc(s.LastUserMsg, 200)))
		}
		if summary, ok := summaryCache[s.ID]; ok {
			fmt.Fprintf(&b, "\n[aqua]── AI 요약 ──[-]\n%s\n", esc(summary))
		} else {
			fmt.Fprintf(&b, "\n[gray][yellow]i[-][gray] 키를 눌러 AI 요약 생성[-]\n")
		}
		infoView.SetText(b.String())
		infoView.ScrollToBeginning()

		var conv strings.Builder
		for _, msg := range s.Messages {
			content := msg.Content
			if len(content) > 500 {
				content = content[:497] + "..."
			}
			if msg.Type == "user" {
				conv.WriteString(fmt.Sprintf("[green]>>> 사용자:[-]\n%s\n\n", esc(content)))
			} else {
				conv.WriteString(fmt.Sprintf("[cyan]<<< 어시스턴트:[-]\n%s\n\n", esc(content)))
			}
		}
		convView.SetText(conv.String())
		convView.ScrollToBeginning()
	}

	togglePreview = func() {
		if previewOpen {
			mainBody.RemoveItem(rightPane)
			previewOpen = false
			app.SetFocus(tree)
		} else {
			mainBody.AddItem(rightPane, 0, 1, false)
			previewOpen = true
			cur := tree.GetCurrentNode()
			if s := nodeSession(cur); s != nil {
				showSessionInfo(s)
			}
		}
	}

	// ── Populate tree ──

	defaultStatus := func() string {
		selected := 0
		totalMsgs := 0
		claudeCount := 0
		codexCount := 0
		expiring := 0
		active := 0
		for _, s := range sessions {
			if s.Selected {
				selected++
			}
			totalMsgs += s.MessageCount
			if s.Provider == ProviderCodex {
				codexCount++
			} else {
				claudeCount++
			}
			d := daysUntilExpiry(s)
			if d >= 0 && d <= 3 {
				expiring++
			}
			if s.Active {
				active++
			}
		}
		sel := ""
		if selected > 0 {
			sel = fmt.Sprintf(" | [red]%d개 선택됨[-]", selected)
		}
		expWarn := ""
		if expiring > 0 {
			expWarn = fmt.Sprintf(" | [red]⚠ %d개 만료 임박[-]", expiring)
		}
		activeInfo := ""
		if active > 0 {
			activeInfo = fmt.Sprintf(" | [lime]▶ %d 활성[-]", active)
		}
		updateNote := ""
		if updateInfo != "" {
			updateNote = " | " + updateInfo
		}
		compactNote := ""
		if compactMode {
			compactNote = " | [aqua]컴팩트[-]"
		}
		return fmt.Sprintf(
			"[green]%d개 세션[-] [gray](🧠%d 🤖%d · %d개 메시지 · %s · %s)[-]%s%s%s%s%s",
			len(sessions), claudeCount, codexCount, totalMsgs, activeBackend, sortLabels[sortMode], sel, activeInfo, expWarn, compactNote, updateNote)
	}

	// Track the previously selected session ID to restore after rebuild
	var lastSelectedID string
	// Numbered session nodes for quick access (1-9)
	var numberedNodes []*tview.TreeNode
	sessionNum := 0

	populateTree := func(filter string) {
		// Remember currently selected session
		if cur := tree.GetCurrentNode(); cur != nil {
			if s := nodeSession(cur); s != nil {
				lastSelectedID = s.ID
			}
		}

		root := tview.NewTreeNode("root")
		tree.SetRoot(root)

		lowerFilter := strings.ToLower(filter)

		var claudeSessions, codexSessions []*Session
		for _, s := range sessions {
			if lowerFilter != "" {
				haystack := strings.ToLower(s.ProjectName + " " + s.Alias + " " + s.FirstUserMsg + " " + s.LastUserMsg + " " + s.GitBranch)
				if !strings.Contains(haystack, lowerFilter) {
					continue
				}
			}
			if s.Provider == ProviderCodex {
				codexSessions = append(codexSessions, s)
			} else {
				claudeSessions = append(claudeSessions, s)
			}
		}

		var firstSessionNode *tview.TreeNode
		var restoreNode *tview.TreeNode
		numberedNodes = nil
		sessionNum = 0

		addProviderGroup := func(provName string, provColor string, icon string, items []*Session, newRef string) {
			if len(items) == 0 {
				return
			}
			// Provider header node
			provNode := tview.NewTreeNode(fmt.Sprintf("%s%s %s[-] (%d)", provColor, provName, icon, len(items)))
			provNode.SetSelectable(false)
			provNode.SetExpanded(true)
			root.AddChild(provNode)

			// "+ New session" node
			newNode := tview.NewTreeNode(fmt.Sprintf("  [#888888]+ 새 %s 세션[-]", provName))
			newNode.SetReference(newRef)
			newNode.SetSelectable(true)
			newNode.SetSelectedTextStyle(selectedStyle)
			provNode.AddChild(newNode)
			if firstSessionNode == nil {
				firstSessionNode = newNode
			}

			// Split into pinned / normal
			var pinned, normal []*Session
			for _, s := range items {
				if s.Pinned {
					pinned = append(pinned, s)
				} else {
					normal = append(normal, s)
				}
			}

			// Pinned group
			if len(pinned) > 0 {
				pinGroupNode := tview.NewTreeNode(fmt.Sprintf("[#444444]고정 \xf0\x9f\x93\x8c (%d)[-]", len(pinned)))
				pinGroupNode.SetSelectable(true)
				pinGroupNode.SetExpanded(true)
				pinGroupNode.SetSelectedTextStyle(selectedStyle)
				provNode.AddChild(pinGroupNode)
				for _, s := range pinned {
					sessionNum++
					numPrefix := ""
					if sessionNum <= 9 {
						numPrefix = fmt.Sprintf("[#666666]%d[-] ", sessionNum)
					}
					nodeText := sessionNodeText(s, filter)
					if compactMode {
						nodeText = sessionNodeTextCompact(s, filter)
					}
					sNode := tview.NewTreeNode(numPrefix + nodeText)
					sNode.SetReference(s)
					sNode.SetSelectable(true)
					sNode.SetSelectedTextStyle(selectedStyle)
					pinGroupNode.AddChild(sNode)
					if sessionNum <= 9 {
						numberedNodes = append(numberedNodes, sNode)
					}
					if firstSessionNode == nil {
						firstSessionNode = sNode
					}
					if lastSelectedID != "" && s.ID == lastSelectedID {
						restoreNode = sNode
					}
				}
			}

			// Normal group — grouped by project
			if len(normal) > 0 {
				normGroupNode := tview.NewTreeNode(fmt.Sprintf("[#444444]세션 (%d)[-]", len(normal)))
				normGroupNode.SetSelectable(true)
				normGroupNode.SetExpanded(true)
				normGroupNode.SetSelectedTextStyle(selectedStyle)
				provNode.AddChild(normGroupNode)

				// Group by project name
				projectOrder := []string{}
				projectMap := make(map[string][]*Session)
				for _, s := range normal {
					if _, exists := projectMap[s.ProjectName]; !exists {
						projectOrder = append(projectOrder, s.ProjectName)
					}
					projectMap[s.ProjectName] = append(projectMap[s.ProjectName], s)
				}
				// Sort: 미분류 always last
				sort.SliceStable(projectOrder, func(i, j int) bool {
					if projectOrder[i] == "미분류" {
						return false
					}
					if projectOrder[j] == "미분류" {
						return true
					}
					return false // keep original order
				})
				for _, projName := range projectOrder {
					projSessions := projectMap[projName]
					displayProj := projName
					if pa, ok := projectAliases[projName]; ok {
						displayProj = pa
					}
					projNode := tview.NewTreeNode(fmt.Sprintf("[#888888]%s[-] (%d)", esc(displayProj), len(projSessions)))
					projNode.SetReference("proj:" + projName)
					projNode.SetSelectable(true)
					projNode.SetExpanded(true)
					projNode.SetSelectedTextStyle(selectedStyle)
					normGroupNode.AddChild(projNode)
					for _, s := range projSessions {
						sessionNum++
						numPrefix := ""
						if sessionNum <= 9 {
							numPrefix = fmt.Sprintf("[#666666]%d[-] ", sessionNum)
						}
						nodeText := sessionNodeText(s, filter)
						if compactMode {
							nodeText = sessionNodeTextCompact(s, filter)
						}
						sNode := tview.NewTreeNode(numPrefix + nodeText)
						sNode.SetReference(s)
						sNode.SetSelectable(true)
						sNode.SetSelectedTextStyle(selectedStyle)
						projNode.AddChild(sNode)
						if sessionNum <= 9 {
							numberedNodes = append(numberedNodes, sNode)
						}
						if firstSessionNode == nil {
							firstSessionNode = sNode
						}
						if lastSelectedID != "" && s.ID == lastSelectedID {
							restoreNode = sNode
						}
					}
				}
			}
		}

		if len(claudeSessions) == 0 && len(codexSessions) == 0 && filter == "" {
			emptyNode := tview.NewTreeNode("[#888888]세션이 없습니다[-]")
			emptyNode.SetSelectable(false)
			root.AddChild(emptyNode)
			guideNode := tview.NewTreeNode("[#888888]터미널에서 [white]claude[-][#888888] 또는 [white]codex[-][#888888]를 실행하면 세션이 생성됩니다[-]")
			guideNode.SetSelectable(false)
			root.AddChild(guideNode)
		}
		addProviderGroup("Claude", "[#FF8C00]", "\xf0\x9f\xa7\xa0", claudeSessions, "new-claude")
		addProviderGroup("Codex", "[#4A9EFF]", "\xf0\x9f\xa4\x96", codexSessions, "new-codex")

		// Restore selection or pick first
		if restoreNode != nil {
			tree.SetCurrentNode(restoreNode)
			if s := nodeSession(restoreNode); s != nil && previewOpen {
				showSessionInfo(s)
			}
		} else if firstSessionNode != nil {
			tree.SetCurrentNode(firstSessionNode)
			if s := nodeSession(firstSessionNode); s != nil && previewOpen {
				showSessionInfo(s)
			}
		} else {
			infoView.SetText("[gray]검색 결과 없음[-]")
			convView.SetText("")
		}

		if filter == "" {
			statusBar.SetText(defaultStatus())
		} else {
			total := len(claudeSessions) + len(codexSessions)
			statusBar.SetText(fmt.Sprintf(
				"[green]%d/%d개 세션[-] | [yellow]Esc[-] 취소 | [yellow]Enter[-] 열기 | [yellow]i[-] 요약",
				total, len(sessions),
			))
		}
	}
	populateTree("")

	tree.SetChangedFunc(func(node *tview.TreeNode) {
		if previewOpen {
			if s := nodeSession(node); s != nil {
				showSessionInfo(s)
			}
		}
	})

	// ── Actions ──

	newSessionInDir := func(provider Provider, dir string) {
		var cmd, label string
		if provider == ProviderCodex {
			cmd = "codex --sandbox danger-full-access"
			label = "Codex"
		} else {
			cmd = "claude --dangerously-skip-permissions"
			label = "Claude"
		}
		err := openInTerminal(cmd, dir, true, app)
		if err != nil {
			statusBar.SetText(fmt.Sprintf("[red]실패: %v[-]", err))
		} else {
			statusBar.SetText(fmt.Sprintf("[green]새 %s 세션 (%s)[-]", label, lastSegment(dir)))
			go func() {
				time.Sleep(2 * time.Second)
				fresh := discoverSessions()
				app.QueueUpdateDraw(func() {
					sessions = fresh
					aliases = loadAliases()
					sortSessions()
					populateTree(currentFilter)
				})
			}()
		}
	}

	newSession := func(provider Provider) {
		home, _ := os.UserHomeDir()

		// Collect unique project dirs for this provider
		projectDirs := make(map[string]string) // name → path
		var projectNames []string
		for _, s := range sessions {
			if s.Provider == provider && s.ProjectDir != "" {
				name := s.ProjectName
				if _, exists := projectDirs[name]; !exists {
					// Verify dir exists
					if _, err := os.Stat(s.ProjectDir); err == nil {
						projectDirs[name] = s.ProjectDir
						projectNames = append(projectNames, name)
					}
				}
			}
		}

		// If only home dir or no projects, open directly
		if len(projectDirs) <= 1 {
			newSessionInDir(provider, home)
			return
		}

		// Show project selection list (vertical)
		label := "Claude"
		if provider == ProviderCodex {
			label = "Codex"
		}

		pickList := tview.NewList().
			ShowSecondaryText(false).
			SetHighlightFullLine(true).
			SetSelectedStyle(tcell.StyleDefault.Background(tcell.ColorGreen).Foreground(tcell.ColorWhite))

		// Add home first
		pickList.AddItem("  홈 ("+lastSegment(home)+")", "", 0, nil)
		pickDirs := []string{home}
		for _, name := range projectNames {
			if projectDirs[name] != home {
				pickList.AddItem("  "+name, "", 0, nil)
				pickDirs = append(pickDirs, projectDirs[name])
			}
		}

		pickList.SetSelectedFunc(func(idx int, _, _ string, _ rune) {
			if idx < len(pickDirs) {
				newSessionInDir(provider, pickDirs[idx])
			}
			app.SetRoot(mainLayout, true)
			app.SetFocus(tree)
		})
		pickList.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
			if ev.Key() == tcell.KeyEscape {
				app.SetRoot(mainLayout, true)
				app.SetFocus(tree)
				return nil
			}
			return ev
		})
		pickList.SetBorder(true).
			SetTitle(fmt.Sprintf(" 새 %s 세션 — 프로젝트 선택 ", label)).
			SetTitleAlign(tview.AlignCenter).
			SetBorderColor(tcell.ColorGreen).
			SetBackgroundColor(tcell.ColorDarkSlateGray)

		modalFlex := tview.NewFlex().SetDirection(tview.FlexRow).
			AddItem(nil, 0, 1, false).
			AddItem(tview.NewFlex().
				AddItem(nil, 0, 1, false).
				AddItem(pickList, 40, 0, true).
				AddItem(nil, 0, 1, false), len(pickDirs)+2, 0, true).
			AddItem(nil, 0, 1, false)
		pages := tview.NewPages().
			AddPage("main", mainLayout, true, true).
			AddPage("pick", modalFlex, true, true)
		app.SetRoot(pages, true)
		app.SetFocus(pickList)
	}

	openSessionFromNode := func(node *tview.TreeNode, inTab bool) {
		if node == nil {
			return
		}
		// Check for "new session" references
		ref := nodeRefStr(node)
		if ref == "new-claude" {
			newSession(ProviderClaude)
			return
		}
		if ref == "new-codex" {
			newSession(ProviderCodex)
			return
		}
		s := nodeSession(node)
		if s == nil {
			// Toggle expand/collapse for group nodes
			node.SetExpanded(!node.IsExpanded())
			return
		}
		var resumeCmd string
		if s.Provider == ProviderCodex {
			resumeCmd = fmt.Sprintf("codex resume %s --sandbox danger-full-access", s.ID)
		} else {
			resumeCmd = fmt.Sprintf("claude --resume %s --dangerously-skip-permissions", s.ID)
		}
		dir := s.ProjectDir
		if _, statErr := os.Stat(dir); statErr != nil {
			if s.CWD != "" {
				dir = s.CWD
			}
			if _, statErr := os.Stat(dir); statErr != nil {
				dir, _ = os.UserHomeDir()
			}
		}
		err := openInTerminal(resumeCmd, dir, inTab, app)
		if err != nil {
			statusBar.SetText(fmt.Sprintf("[red]실패 (%s): %v[-]", activeBackend, err))
		} else {
			mode := "tab"
			if !inTab {
				mode = "split"
			}
			statusBar.SetText(fmt.Sprintf("[green]%s 열림 (%s %s)[-]", esc(s.ProjectName), activeBackend, mode))
			// Auto-refresh after opening so the resumed session moves to top
			doRefresh := func() {
				fresh := discoverSessions()
				app.QueueUpdateDraw(func() {
					sessions = fresh
					aliases = loadAliases()
					populateTree(currentFilter)
				})
			}
			go func() {
				time.Sleep(2 * time.Second)
				doRefresh()
				time.Sleep(5 * time.Second)
				doRefresh()
				time.Sleep(10 * time.Second)
				doRefresh()
			}()
		}
	}

	requestSummary := func() {
		cur := tree.GetCurrentNode()
		s := nodeSession(cur)
		if s == nil {
			return
		}
		if _, ok := summaryCache[s.ID]; ok {
			showSessionInfo(s)
			return
		}
		sessionID := s.ID
		statusBar.SetText(fmt.Sprintf("[yellow]%s 요약 생성 중...[-]", esc(s.ProjectName)))
		infoView.SetText(infoView.GetText(false) + "\n[yellow]AI 요약 생성 중...[-]")
		go func() {
			summary, err := generateSummary(s)
			app.QueueUpdateDraw(func() {
				if err != nil {
					statusBar.SetText(fmt.Sprintf("[red]요약 실패: %v[-]", err))
					return
				}
				summaryCache[sessionID] = summary
				curNode := tree.GetCurrentNode()
				if cs := nodeSession(curNode); cs != nil && cs.ID == sessionID {
					showSessionInfo(cs)
				}
				statusBar.SetText(fmt.Sprintf("[green]요약 완료: %s[-]", esc(s.ProjectName)))
			})
		}()
	}

	tree.SetSelectedFunc(func(node *tview.TreeNode) {
		openSessionFromNode(node, true)
	})

	// ── Focus & search ──

	focusables := []tview.Primitive{tree, convView}
	focusIdx := 0
	searching := false

	updateBorders := func() {
		if focusIdx == 0 {
			tree.SetBorderColor(tcell.ColorGreen)
			convView.SetBorderColor(tcell.ColorDodgerBlue)
		} else {
			tree.SetBorderColor(tcell.ColorDodgerBlue)
			convView.SetBorderColor(tcell.ColorGreen)
		}
	}

	showSearch := func() {
		searching = true
		leftPane.Clear()
		leftPane.AddItem(searchInput, 1, 0, false)
		leftPane.AddItem(tree, 0, 1, false)
		app.SetFocus(searchInput)
	}

	hideSearch := func() {
		searching = false
		leftPane.Clear()
		leftPane.AddItem(tree, 0, 1, true)
		searchInput.SetText("")
		currentFilter = ""
		populateTree("")
		focusIdx = 0
		app.SetFocus(tree)
		updateBorders()
	}

	searchInput.SetDoneFunc(func(key tcell.Key) {
		if key == tcell.KeyEscape {
			hideSearch()
		} else if key == tcell.KeyEnter {
			focusIdx = 0
			app.SetFocus(tree)
			updateBorders()
		}
	})

	searchInput.SetChangedFunc(func(text string) {
		currentFilter = text
		populateTree(text)
	})

	// ── Key bindings ──

	app.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		// Skip key mapping when in input field or modal
		focused := app.GetFocus()
		if _, ok := focused.(*tview.InputField); ok {
			return ev
		}
		if _, ok := focused.(*tview.Button); ok {
			return ev
		}

		switch ev.Key() {
		case tcell.KeyTab:
			if previewOpen {
				focusIdx = (focusIdx + 1) % len(focusables)
				app.SetFocus(focusables[focusIdx])
				updateBorders()
			}
			return nil
		case tcell.KeyEscape:
			if searching {
				hideSearch()
				return nil
			}
			if previewOpen {
				togglePreview()
				return nil
			}
			app.Stop()
			return nil
		case tcell.KeyRune:
			if focusIdx == 0 {
				switch toEngKey(ev.Rune()) {
				case '1', '2', '3', '4', '5', '6', '7', '8', '9':
					idx := int(toEngKey(ev.Rune()) - '1')
					if idx >= 0 && idx < len(numberedNodes) {
						tree.SetCurrentNode(numberedNodes[idx])
						openSessionFromNode(numberedNodes[idx], true)
					}
					return nil

				case '/':
					showSearch()
					return nil

				case '?': // Help
					helpText := tview.NewTextView().
						SetDynamicColors(true).
						SetTextAlign(tview.AlignLeft).
						SetWordWrap(false)
					helpText.SetText(
						"[yellow]세션 매니저 도움말[-]\n\n" +
							"[white]날짜 색상:[-]\n" +
							"  [#00BFFF]파랑[-]   최근 2분 (활성)\n" +
							"  [#00ff00]초록[-]   최근 7일\n" +
							"  [#666666]회색[-]   7일 이상\n" +
							"  [#FF4444]빨강[-]   만료 (30일+)\n\n" +
							"[white]아이콘:[-]\n" +
							"  Claude [#FF8C00]\xf0\x9f\xa7\xa0[-]  세션\n" +
							"  Codex [#4A9EFF]\xf0\x9f\xa4\x96[-]  세션\n" +
							"  \xe2\x96\xb6  활성 (사용 중)\n\n" +
							"[white]열 구성:[-]  아이콘 | CLI/DSK/WEB | D-day | 날짜 | 프로젝트 | 제목\n\n" +
							"[white]D-day:[-]  세션 만료까지 남은 일수 (기준 30일)\n" +
							"  D-29 = 29일 남음  |  D+3 = 3일 전 만료\n\n" +
							"[white]단축키:[-]\n" +
							"  Enter=열기    p=미리보기   Space=선택   m=이름변경\n" +
							"  d=삭제       D=일괄삭제   E=일괄내보내기  e=내보내기\n" +
							"  c=컴팩트     o=폴더      /=검색       r=새로고침\n" +
							"  t=고정       s=정렬      x=휴지통     Esc=종료\n\n" +
							"[gray]아무 키나 누르면 닫힘[-]",
					)
					helpText.SetBorder(true).
						SetTitle(" 도움말 ").
						SetTitleAlign(tview.AlignCenter).
						SetBorderColor(tcell.ColorDodgerBlue).
						SetBackgroundColor(tcell.ColorDarkSlateGray)
					helpText.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
						app.SetRoot(mainLayout, true)
						app.SetFocus(tree)
						return nil
					})
					helpPage := tview.NewFlex().SetDirection(tview.FlexRow).
						AddItem(nil, 0, 1, false).
						AddItem(tview.NewFlex().
							AddItem(nil, 0, 1, false).
							AddItem(helpText, 65, 0, true).
							AddItem(nil, 0, 1, false), 22, 0, true).
						AddItem(nil, 0, 1, false)
					pages := tview.NewPages().
						AddPage("main", mainLayout, true, true).
						AddPage("help", helpPage, true, true)
					app.SetRoot(pages, true)
					app.SetFocus(helpText)
					return nil

				case 'i':
					requestSummary()
					return nil

				case 'p': // Toggle preview panel
					togglePreview()
					return nil

				case 't': // Toggle pin
					cur := tree.GetCurrentNode()
					s := nodeSession(cur)
					if s == nil {
						return nil
					}
					// Also sync with Codex desktop
					codexPins := loadCodexPins()
					if s.Pinned {
						s.Pinned = false
						delete(localPins, s.ID)
						delete(codexPins, s.ID)
						localUnpins[s.ID] = true
						statusBar.SetText(fmt.Sprintf("[yellow]고정 해제: %s[-]", esc(s.ProjectName)))
					} else {
						s.Pinned = true
						localPins[s.ID] = true
						delete(localUnpins, s.ID)
						if s.Provider == ProviderCodex {
							codexPins[s.ID] = true
						}
						statusBar.SetText(fmt.Sprintf("[green]📌 고정: %s[-]", esc(s.ProjectName)))
					}
					savePins(localPins)
					saveUnpins(localUnpins)
					saveCodexPins(codexPins)
					sessions = discoverSessions()
					sortSessions()
					populateTree(currentFilter)
					return nil

				case 's': // Sort toggle
					sortMode = (sortMode + 1) % 3
					sortSessions()
					populateTree(currentFilter)
					statusBar.SetText(fmt.Sprintf("[green]정렬: %s[-]", sortLabels[sortMode]))
					return nil

				case 'r': // Refresh
					go func() {
						app.QueueUpdateDraw(func() {
							statusBar.SetText("[yellow]새로고침 중...[-]")
						})
						fresh := discoverSessions()
						app.QueueUpdateDraw(func() {
							sessions = fresh
							aliases = loadAliases()
							populateTree(currentFilter)
						})
					}()
					return nil

				case ' ': // Toggle multi-select
					cur := tree.GetCurrentNode()
					s := nodeSession(cur)
					if s == nil {
						return nil
					}
					s.Selected = !s.Selected
					if compactMode {
						cur.SetText(sessionNodeTextCompact(s, currentFilter))
					} else {
						cur.SetText(sessionNodeText(s, currentFilter))
					}
					statusBar.SetText(defaultStatus())
					return nil

				case 'm': // Rename (alias) — session or project group
					cur := tree.GetCurrentNode()

					// Check if it's a project group node
					refStr := nodeRefStr(cur)
					if strings.HasPrefix(refStr, "proj:") {
						projName := strings.TrimPrefix(refStr, "proj:")
						currentAlias := projectAliases[projName]
						renameInput := tview.NewInputField().
							SetLabel(" 새 이름: ").
							SetText(currentAlias).
							SetFieldWidth(40).
							SetFieldBackgroundColor(tcell.ColorDarkSlateGray)
						renameInput.SetBorder(true).
							SetTitle(" 프로젝트 이름 변경 ").
							SetTitleAlign(tview.AlignCenter)
						renameInput.SetDoneFunc(func(key tcell.Key) {
							if key == tcell.KeyEnter {
								newAlias := strings.TrimSpace(renameInput.GetText())
								if newAlias == "" {
									delete(projectAliases, projName)
								} else {
									projectAliases[projName] = newAlias
								}
								saveProjectAliases(projectAliases)
								populateTree(currentFilter)
								if newAlias != "" {
									statusBar.SetText(fmt.Sprintf("[green]프로젝트 이름 변경: %s → %s[-]", esc(projName), esc(newAlias)))
								} else {
									statusBar.SetText("[green]프로젝트 별칭 삭제됨[-]")
								}
							}
							app.SetRoot(mainLayout, true)
							app.SetFocus(tree)
						})
						modal := tview.NewFlex().SetDirection(tview.FlexRow).
							AddItem(nil, 0, 1, false).
							AddItem(tview.NewFlex().
								AddItem(nil, 0, 1, false).
								AddItem(renameInput, 50, 0, true).
								AddItem(nil, 0, 1, false), 3, 0, true).
							AddItem(nil, 0, 1, false)
						pages := tview.NewPages().
							AddPage("main", mainLayout, true, true).
							AddPage("rename", modal, true, true)
						app.SetRoot(pages, true)
						app.SetFocus(renameInput)
						return nil
					}

					s := nodeSession(cur)
					if s == nil {
						return nil
					}
					renameInput := tview.NewInputField().
						SetLabel(" 새 이름: ").
						SetText(s.Alias).
						SetFieldWidth(40).
						SetFieldBackgroundColor(tcell.ColorDarkSlateGray)
					renameInput.SetBorder(true).
						SetTitle(" 세션 이름 변경 ").
						SetTitleAlign(tview.AlignCenter)
					renameInput.SetDoneFunc(func(key tcell.Key) {
						if key == tcell.KeyEnter {
							newAlias := strings.TrimSpace(renameInput.GetText())
							s.Alias = newAlias
							if newAlias == "" {
								delete(aliases, s.ID)
							} else {
								aliases[s.ID] = newAlias
							}
							saveAliases(aliases)
							sessions = discoverSessions()
							populateTree(currentFilter)
							if newAlias != "" {
								statusBar.SetText(fmt.Sprintf("[green]이름 변경: %s[-]", esc(newAlias)))
							} else {
								statusBar.SetText("[green]별칭 삭제됨[-]")
							}
						}
						app.SetRoot(mainLayout, true)
						app.SetFocus(tree)
					})
					modal := tview.NewFlex().SetDirection(tview.FlexRow).
						AddItem(nil, 0, 1, false).
						AddItem(tview.NewFlex().
							AddItem(nil, 0, 1, false).
							AddItem(renameInput, 50, 0, true).
							AddItem(nil, 0, 1, false), 3, 0, true).
						AddItem(nil, 0, 1, false)
					pages := tview.NewPages().
						AddPage("main", mainLayout, true, true).
						AddPage("rename", modal, true, true)
					app.SetRoot(pages, true)
					app.SetFocus(renameInput)
					return nil

				case 'd': // Delete session or group
					cur := tree.GetCurrentNode()
					s := nodeSession(cur)

					// Check if it's a group node with children sessions
					if s == nil && cur != nil {
						children := cur.GetChildren()
						var groupSessions []*Session
						for _, child := range children {
							if cs := nodeSession(child); cs != nil {
								groupSessions = append(groupSessions, cs)
							}
						}
						if len(groupSessions) == 0 {
							return nil
						}
						groupName := cur.GetText()
						confirmModal := tview.NewModal().
							SetText(fmt.Sprintf("%s\n\n%d개 세션을 삭제하시겠습니까?", groupName, len(groupSessions))).
							AddButtons([]string{"삭제", "취소"}).
							SetDoneFunc(func(_ int, label string) {
								if label == "삭제" {
									deleted := 0
									for _, gs := range groupSessions {
										if deleteSession(gs) == nil {
											delete(aliases, gs.ID)
											deleted++
										}
									}
									saveAliases(aliases)
									sessions = discoverSessions()
									sortSessions()
									populateTree(currentFilter)
									statusBar.SetText(fmt.Sprintf("[green]%d개 세션 삭제됨[-]", deleted))
								}
								app.SetRoot(mainLayout, true)
								app.SetFocus(tree)
							})
						confirmModal.SetBackgroundColor(tcell.ColorDarkSlateGray)
						confirmModal.SetButtonBackgroundColor(tcell.NewRGBColor(40, 40, 40))
						confirmModal.SetButtonTextColor(tcell.ColorGray)
						confirmModal.SetButtonActivatedStyle(tcell.StyleDefault.Background(tcell.ColorRed).Foreground(tcell.ColorWhite).Bold(true))
						confirmModal.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
							if ev.Key() == tcell.KeyRune && toEngKey(ev.Rune()) == 'd' {
								deleted := 0
								for _, gs := range groupSessions {
									if deleteSession(gs) == nil {
										delete(aliases, gs.ID)
										deleted++
									}
								}
								saveAliases(aliases)
								sessions = discoverSessions()
								sortSessions()
								populateTree(currentFilter)
								statusBar.SetText(fmt.Sprintf("[green]%d개 세션 삭제됨[-]", deleted))
								app.SetRoot(mainLayout, true)
								app.SetFocus(tree)
								return nil
							}
							return ev
						})
						app.SetRoot(confirmModal, true)
						return nil
					}

					if s == nil {
						return nil
					}
					displayName := s.ProjectName
					if s.Alias != "" {
						displayName = s.Alias
					}
					confirmModal := tview.NewModal().
						SetText(fmt.Sprintf("세션을 삭제하시겠습니까?\n\n%s\n%s", displayName, s.ID)).
						AddButtons([]string{"삭제", "취소"}).
						SetDoneFunc(func(_ int, label string) {
							if label == "삭제" {
								if err := deleteSession(s); err != nil {
									statusBar.SetText(fmt.Sprintf("[red]삭제 실패: %v[-]", err))
								} else {
									delete(aliases, s.ID)
									saveAliases(aliases)
									sessions = discoverSessions()
									sortSessions()
									populateTree(currentFilter)
									statusBar.SetText(fmt.Sprintf("[green]삭제됨: %s[-]", esc(displayName)))
								}
							}
							app.SetRoot(mainLayout, true)
							app.SetFocus(tree)
						})
					confirmModal.SetBackgroundColor(tcell.ColorDarkSlateGray)
					confirmModal.SetButtonBackgroundColor(tcell.NewRGBColor(40, 40, 40))
					confirmModal.SetButtonTextColor(tcell.ColorGray)
					confirmModal.SetButtonActivatedStyle(tcell.StyleDefault.Background(tcell.ColorRed).Foreground(tcell.ColorWhite).Bold(true))
					// Allow 'd' key to confirm delete in modal
					confirmModal.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
						if ev.Key() == tcell.KeyRune && toEngKey(ev.Rune()) == 'd' {
							if err := deleteSession(s); err != nil {
								statusBar.SetText(fmt.Sprintf("[red]삭제 실패: %v[-]", err))
							} else {
								delete(aliases, s.ID)
								saveAliases(aliases)
								sessions = discoverSessions()
								sortSessions()
								populateTree(currentFilter)
								statusBar.SetText(fmt.Sprintf("[green]삭제됨: %s[-]", esc(displayName)))
							}
							app.SetRoot(mainLayout, true)
							app.SetFocus(tree)
							return nil
						}
						return ev
					})
					app.SetRoot(confirmModal, true)
					return nil

				case 'o': // Open session folder in Finder
					cur := tree.GetCurrentNode()
					s := nodeSession(cur)
					if s == nil {
						return nil
					}
					dir := filepath.Dir(s.SessionFile)
					if err := exec.Command("open", dir).Run(); err != nil {
						statusBar.SetText(fmt.Sprintf("[red]폴더 열기 실패: %v[-]", err))
					} else {
						statusBar.SetText(fmt.Sprintf("[green]폴더 열림: %s[-]", esc(dir)))
					}
					return nil

				case 'x': // Trash view
					trashItems := listTrash()
					if len(trashItems) == 0 {
						statusBar.SetText("[yellow]휴지통이 비어있습니다[-]")
						return nil
					}
					trashList := tview.NewList().
						ShowSecondaryText(false).
						SetHighlightFullLine(true).
						SetSelectedStyle(tcell.StyleDefault.Background(tcell.ColorDarkSlateGray).Foreground(tcell.ColorWhite))
					for _, item := range trashItems {
						label := fmt.Sprintf("  [#999999]%s[-]  [white]%s[-]", item["deletedAt"][:10], filepath.Base(item["trashFile"]))
						trashList.AddItem(label, "", 0, nil)
					}
					trashList.SetSelectedFunc(func(idx int, _, _ string, _ rune) {
						if idx >= len(trashItems) {
							return
						}
						item := trashItems[idx]
						actionModal := tview.NewModal().
							SetText(fmt.Sprintf("세션: %s", filepath.Base(item["trashFile"]))).
							AddButtons([]string{"복원", "영구 삭제", "취소"}).
							SetDoneFunc(func(_ int, label string) {
								switch label {
								case "복원":
									if err := restoreFromTrash(item); err != nil {
										statusBar.SetText(fmt.Sprintf("[red]복원 실패: %v[-]", err))
									} else {
										sessions = discoverSessions()
										sortSessions()
										populateTree(currentFilter)
										statusBar.SetText("[green]세션 복원됨[-]")
									}
								case "영구 삭제":
									permanentDeleteTrash(item)
									statusBar.SetText("[green]영구 삭제됨[-]")
								}
								app.SetRoot(mainLayout, true)
								app.SetFocus(tree)
							})
						actionModal.SetBackgroundColor(tcell.ColorDarkSlateGray)
						actionModal.SetButtonBackgroundColor(tcell.NewRGBColor(40, 40, 40))
						actionModal.SetButtonTextColor(tcell.ColorGray)
						actionModal.SetButtonActivatedStyle(tcell.StyleDefault.Background(tcell.ColorDodgerBlue).Foreground(tcell.ColorWhite).Bold(true))
						app.SetRoot(actionModal, true)
					})
					trashList.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
						if ev.Key() == tcell.KeyEscape {
							app.SetRoot(mainLayout, true)
							app.SetFocus(tree)
							return nil
						}
						return ev
					})
					trashList.SetBorder(true).
						SetTitle(fmt.Sprintf(" 휴지통 (%d) ", len(trashItems))).
						SetTitleAlign(tview.AlignCenter).
						SetBorderColor(tcell.ColorRed)
					trashFlex := tview.NewFlex().SetDirection(tview.FlexRow).
						AddItem(nil, 0, 1, false).
						AddItem(tview.NewFlex().
							AddItem(nil, 0, 1, false).
							AddItem(trashList, 60, 0, true).
							AddItem(nil, 0, 1, false), len(trashItems)+2, 0, true).
						AddItem(nil, 0, 1, false)
					pages := tview.NewPages().
						AddPage("main", mainLayout, true, true).
						AddPage("trash", trashFlex, true, true)
					app.SetRoot(pages, true)
					app.SetFocus(trashList)
					return nil

				case 'e': // Export session to markdown
					cur := tree.GetCurrentNode()
					s := nodeSession(cur)
					if s == nil {
						return nil
					}
					home, _ := os.UserHomeDir()
					title := s.Alias
					if title == "" {
						title = trunc(s.FirstUserMsg, 30)
					}
					if title == "" {
						title = s.ID[:8]
					}
					// Sanitize filename
					safeName := strings.Map(func(r rune) rune {
						if r == '/' || r == '\\' || r == ':' || r == '*' || r == '?' || r == '"' || r == '<' || r == '>' || r == '|' {
							return '_'
						}
						return r
					}, title)
					exportPath := filepath.Join(home, "Desktop", safeName+".md")

					var md strings.Builder
					md.WriteString(fmt.Sprintf("# %s\n\n", title))
					md.WriteString(fmt.Sprintf("- **세션 ID**: %s\n", s.ID))
					md.WriteString(fmt.Sprintf("- **프로젝트**: %s\n", s.ProjectName))
					md.WriteString(fmt.Sprintf("- **날짜**: %s\n", s.ModTime.Format("2006-01-02 15:04:05")))
					md.WriteString(fmt.Sprintf("- **메시지**: %d개\n\n", s.MessageCount))
					md.WriteString("---\n\n")
					for _, msg := range s.Messages {
						if msg.Type == "user" {
							md.WriteString("## 👤 사용자\n\n")
						} else {
							md.WriteString("## 🤖 어시스턴트\n\n")
						}
						md.WriteString(msg.Content + "\n\n")
					}
					if err := os.WriteFile(exportPath, []byte(md.String()), 0644); err != nil {
						statusBar.SetText(fmt.Sprintf("[red]내보내기 실패: %v[-]", err))
					} else {
						statusBar.SetText(fmt.Sprintf("[green]내보내기 완료: %s[-]", esc(exportPath)))
					}
					return nil

				case 'D': // Batch delete selected sessions
					var selected []*Session
					for _, s := range sessions {
						if s.Selected {
							selected = append(selected, s)
						}
					}
					if len(selected) == 0 {
						statusBar.SetText("[yellow]선택된 세션이 없습니다 (Space로 선택)[-]")
						return nil
					}
					confirmModal := tview.NewModal().
						SetText(fmt.Sprintf("%d개 선택된 세션을 삭제하시겠습니까?", len(selected))).
						AddButtons([]string{"삭제", "취소"}).
						SetDoneFunc(func(_ int, label string) {
							if label == "삭제" {
								deleted := 0
								for _, s := range selected {
									if deleteSession(s) == nil {
										delete(aliases, s.ID)
										deleted++
									}
								}
								saveAliases(aliases)
								sessions = discoverSessions()
								sortSessions()
								populateTree(currentFilter)
								statusBar.SetText(fmt.Sprintf("[green]%d개 세션 삭제됨[-]", deleted))
							}
							app.SetRoot(mainLayout, true)
							app.SetFocus(tree)
						})
					confirmModal.SetBackgroundColor(tcell.ColorDarkSlateGray)
					confirmModal.SetButtonBackgroundColor(tcell.NewRGBColor(40, 40, 40))
					confirmModal.SetButtonTextColor(tcell.ColorGray)
					confirmModal.SetButtonActivatedStyle(tcell.StyleDefault.Background(tcell.ColorRed).Foreground(tcell.ColorWhite).Bold(true))
					confirmModal.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
						if ev.Key() == tcell.KeyRune && toEngKey(ev.Rune()) == 'd' {
							deleted := 0
							for _, s := range selected {
								if deleteSession(s) == nil {
									delete(aliases, s.ID)
									deleted++
								}
							}
							saveAliases(aliases)
							sessions = discoverSessions()
							sortSessions()
							populateTree(currentFilter)
							statusBar.SetText(fmt.Sprintf("[green]%d개 세션 삭제됨[-]", deleted))
							app.SetRoot(mainLayout, true)
							app.SetFocus(tree)
							return nil
						}
						return ev
					})
					app.SetRoot(confirmModal, true)
					return nil

				case 'E': // Batch export selected sessions
					var selected []*Session
					for _, s := range sessions {
						if s.Selected {
							selected = append(selected, s)
						}
					}
					if len(selected) == 0 {
						statusBar.SetText("[yellow]선택된 세션이 없습니다 (Space로 선택)[-]")
						return nil
					}
					home, _ := os.UserHomeDir()
					exported := 0
					for _, s := range selected {
						title := s.Alias
						if title == "" {
							title = trunc(s.FirstUserMsg, 30)
						}
						if title == "" {
							title = s.ID[:8]
						}
						safeName := strings.Map(func(r rune) rune {
							if r == '/' || r == '\\' || r == ':' || r == '*' || r == '?' || r == '"' || r == '<' || r == '>' || r == '|' {
								return '_'
							}
							return r
						}, title)
						exportPath := filepath.Join(home, "Desktop", safeName+".md")

						var md strings.Builder
						md.WriteString(fmt.Sprintf("# %s\n\n", title))
						md.WriteString(fmt.Sprintf("- **세션 ID**: %s\n", s.ID))
						md.WriteString(fmt.Sprintf("- **프로젝트**: %s\n", s.ProjectName))
						md.WriteString(fmt.Sprintf("- **날짜**: %s\n", s.ModTime.Format("2006-01-02 15:04:05")))
						md.WriteString(fmt.Sprintf("- **메시지**: %d개\n\n", s.MessageCount))
						md.WriteString("---\n\n")
						for _, msg := range s.Messages {
							if msg.Type == "user" {
								md.WriteString("## 👤 사용자\n\n")
							} else {
								md.WriteString("## 🤖 어시스턴트\n\n")
							}
							md.WriteString(msg.Content + "\n\n")
						}
						if os.WriteFile(exportPath, []byte(md.String()), 0644) == nil {
							exported++
							s.Selected = false
						}
					}
					populateTree(currentFilter)
					statusBar.SetText(fmt.Sprintf("[green]%d개 세션 내보내기 완료 (바탕화면)[-]", exported))
					return nil

				case 'c': // Compact mode toggle
					compactMode = !compactMode
					populateTree(currentFilter)
					if compactMode {
						statusBar.SetText("[aqua]컴팩트 모드 ON[-]")
					} else {
						statusBar.SetText("[green]컴팩트 모드 OFF[-]")
					}
					return nil

				case 'u': // Self-update
					if updateInfo == "" {
						statusBar.SetText("[yellow]업데이트 확인 중...[-]")
						go func() {
							newVer, _, has := checkForUpdate()
							app.QueueUpdateDraw(func() {
								if !has {
									statusBar.SetText("[green]이미 최신 버전입니다 (v" + currentVersion + ")[-]")
								} else {
									updateInfo = fmt.Sprintf("[yellow]⬆ 새 버전 %s 사용 가능[-]", newVer)
									statusBar.SetText(fmt.Sprintf("[yellow]새 버전 %s 발견. 터미널에서 csm --update 실행[-]", newVer))
								}
							})
						}()
					} else {
						statusBar.SetText("[yellow]터미널에서 csm --update 실행하세요[-]")
					}
					return nil
				}
			}
		}
		return ev
	})

	// Background update check
	go func() {
		if newVer, _, has := checkForUpdate(); has {
			app.QueueUpdateDraw(func() {
				updateInfo = fmt.Sprintf("[yellow]⬆ 새 버전 %s 사용 가능[-]", newVer)
				statusBar.SetText(defaultStatus())
			})
		}
	}()

	// Background polling
	go func() {
		ticker := time.NewTicker(time.Duration(cfg.RefreshInterval) * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			fresh := discoverSessions()
			app.QueueUpdateDraw(func() {
				sessions = fresh
				aliases = loadAliases()
				populateTree(currentFilter)
			})
		}
	}()

	if err := app.SetRoot(mainLayout, true).Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
