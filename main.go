package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// ── Constants ───────────────────────────────────────────────────────────────

const sessionExpiryDays = 30 // sessions older than this are considered expired

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

// isSessionActive checks if a session file was modified very recently (likely in use)
func isSessionActive(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return time.Since(info.ModTime()) < 2*time.Minute
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
	projectName := lastSegment(cwd)
	if projectName == "" {
		projectName = "codex"
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
				sess.ProjectName = lastSegment(meta.CWD)
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

func deleteSession(s *Session) error {
	// Delete the session file
	if err := os.Remove(s.SessionFile); err != nil && !os.IsNotExist(err) {
		return err
	}

	// For Codex sessions, also remove from session_index.jsonl
	if s.Provider == ProviderCodex {
		removeFromCodexIndex(s.ID)
	}
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
	// Also load Codex desktop pins
	codexPins := loadCodexPins()
	for id := range codexPins {
		pins[id] = true
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
	sess := &Session{
		ID:          strings.TrimSuffix(filepath.Base(path), ".jsonl"),
		ProjectDir:  ppath,
		ProjectName: lastSegment(ppath),
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

	// Detect active sessions
	for _, s := range out {
		s.Active = isSessionActive(s.SessionFile)
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
	backendFallback
)

func (b termBackend) String() string {
	names := [...]string{"iTerm2", "Terminal.app", "tmux", "Kitty", "WezTerm", "Ghostty", "fallback"}
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

// ── Main ────────────────────────────────────────────────────────────────────

func main() {
	fmt.Print("세션 불러오는 중...")
	sessions := discoverSessions()
	fmt.Print("\r\033[2K")

	activeBackend = detectBackend()
	app := tview.NewApplication()
	summaryCache := make(map[string]string)
	aliases := loadAliases()
	localPins := loadPins()

	// ── Widgets ──

	sessionList := tview.NewList().
		ShowSecondaryText(false).
		SetHighlightFullLine(true).
		SetSelectedStyle(tcell.StyleDefault.Background(tcell.ColorDarkSlateGray).Foreground(tcell.ColorWhite))
	sessionList.SetBorder(true).
		SetTitle(" 세션 목록 ").
		SetTitleAlign(tview.AlignLeft).
		SetBorderColor(tcell.ColorGreen)

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
	helpBar.SetText("[yellow]Enter[-] 열기 | [yellow]p[-] 미리보기 | [yellow]t[-] 고정 | [yellow]Space[-] 선택 | [yellow]m[-] 이름변경 | [yellow]d[-] 삭제 | [yellow]D[-] 일괄삭제\n[yellow]o[-] 폴더 | [yellow]/[-] 검색 | [yellow]r[-] 새로고침 | [yellow]?[-] 도움말 | [yellow]Esc[-] 종료")

	statusBar := tview.NewTextView().
		SetDynamicColors(true).
		SetTextAlign(tview.AlignCenter)

	// ── Layout ──

	leftPane := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(sessionList, 0, 1, true)

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
	var filteredIdx []int
	currentFilter := ""
	collapsed := make(map[string]bool) // "claude", "codex" group collapse state
	// Track which group each list item belongs to
	var itemGroup []string

	// ── Session display ──

	showSessionInfo := func(idx int) {
		if idx < 0 || idx >= len(sessions) {
			return
		}
		s := sessions[idx]

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
			fmt.Fprintf(&b, "[yellow]상태:[-]        [lime]▶ 활성[-]\n")
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
			app.SetFocus(sessionList)
		} else {
			mainBody.AddItem(rightPane, 0, 1, false)
			previewOpen = true
			idx := sessionList.GetCurrentItem()
			if idx >= 0 && idx < len(filteredIdx) && filteredIdx[idx] >= 0 {
				showSessionInfo(filteredIdx[idx])
			}
		}
	}

	// ── Filtered session list ──

	defaultStatus := func() string {
		selected := 0
		for _, s := range sessions {
			if s.Selected {
				selected++
			}
		}
		sel := ""
		if selected > 0 {
			sel = fmt.Sprintf(" | [red]%d개 선택됨[-]", selected)
		}
		return fmt.Sprintf(
			"[green]%d개 세션[-] [gray](%s)[-]%s",
			len(sessions), activeBackend, sel,
		)
	}

	populateList := func(filter string) {
		sessionList.Clear()
		filteredIdx = nil
		itemGroup = nil
		lowerFilter := strings.ToLower(filter)

		var claudeSessions, codexSessions []int
		for i, s := range sessions {
			if lowerFilter != "" {
				haystack := strings.ToLower(s.ProjectName + " " + s.Alias + " " + s.FirstUserMsg + " " + s.LastUserMsg + " " + s.GitBranch)
				if !strings.Contains(haystack, lowerFilter) {
					continue
				}
			}
			if s.Provider == ProviderCodex {
				codexSessions = append(codexSessions, i)
			} else {
				claudeSessions = append(claudeSessions, i)
			}
		}


		addSep := func(title string, group string) {
			sessionList.AddItem(fmt.Sprintf("[#444444]── %s ──[-]", title), "", 0, nil)
			filteredIdx = append(filteredIdx, -1)
			itemGroup = append(itemGroup, group)
		}

		// Tree item with prefix
		addTreeItem := func(si int, isLast bool, group string) {
			s := sessions[si]
			check := ""
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
			dateColor := "[#666666]"
			if daysUntilExpiry(s) < 0 {
				dateColor = "[#FF4444]"
			} else if time.Since(s.ModTime) < 2*time.Minute {
				dateColor = "[#00BFFF]"
			} else if time.Since(s.ModTime) < 7*24*time.Hour {
				dateColor = "[#00ff00]"
			}
			activeIcon := " "
			if s.Active {
				activeIcon = "[lime]\xe2\x96\xb6[-]"
			}

			branch := "[#444444]\xe2\x94\x9c[-]" // ├
			if isLast {
				branch = "[#444444]\xe2\x94\x94[-]" // └
			}

			col1 := fmt.Sprintf("%s%s%s", check, activeIcon, branch)
			col2 := padRight(fmt.Sprintf("%s%s[-]", dateColor, s.ModTime.Format("01/02 15:04")), 12)
			col3 := padRight(fmt.Sprintf("[#666666]%s[-]", esc(s.ProjectName)), 20)
			col4 := padRight(fmt.Sprintf("[white]%s[-]", esc(title)), 30)
			col5 := padRight(fmt.Sprintf("[#666666]%s[-]", epIco), 4)
			col6 := padRight(fmt.Sprintf("[#666666]%s[-]", expiryLabel(s)), 6)
			label := fmt.Sprintf("   %s %s  %s  %s  %s  %s", col1, col2, col3, col4, col5, col6)
			sessionList.AddItem(label, "", 0, nil)
			filteredIdx = append(filteredIdx, si)
			itemGroup = append(itemGroup, group)
		}

		addTreeItems := func(items []int, group string) {
			for i, si := range items {
				addTreeItem(si, i == len(items)-1, group)
			}
		}

		// Split each provider into pinned and unpinned
		addProviderGroup := func(provName string, provColor string, icon string, items []int, newIdx int) {
			if len(items) == 0 {
				return
			}
			groupKey := strings.ToLower(provName)
			var pinned, normal []int
			for _, si := range items {
				if sessions[si].Pinned {
					pinned = append(pinned, si)
				} else {
					normal = append(normal, si)
				}
			}

			// Provider header (no toggle)
			addSep(fmt.Sprintf("%s%s %s[-][#444444] (%d)", provColor, provName, icon, len(items)), groupKey)

			// New session
			sessionList.AddItem(fmt.Sprintf("   %s+ 새 %s 세션[-]", provColor, provName), "", 0, nil)
			filteredIdx = append(filteredIdx, newIdx)
			itemGroup = append(itemGroup, groupKey)

			// Pinned
			if len(pinned) > 0 {
				pinKey := groupKey + "-pin"
				pinArrow := "\xe2\x96\xbc" // ▼
				if collapsed[pinKey] {
					pinArrow = "\xe2\x96\xb6" // ▶
				}
				sessionList.AddItem(fmt.Sprintf("   [#444444]%s 고정 \xf0\x9f\x93\x8c (%d)[-]", pinArrow, len(pinned)), "", 0, nil)
				filteredIdx = append(filteredIdx, -1)
				itemGroup = append(itemGroup, pinKey)
				if !collapsed[pinKey] {
					addTreeItems(pinned, pinKey)
				}
			}

			// Normal
			if len(normal) > 0 {
				normKey := groupKey + "-normal"
				normArrow := "\xe2\x96\xbc" // ▼
				if collapsed[normKey] {
					normArrow = "\xe2\x96\xb6" // ▶
				}
				sessionList.AddItem(fmt.Sprintf("   [#444444]%s 세션 (%d)[-]", normArrow, len(normal)), "", 0, nil)
				filteredIdx = append(filteredIdx, -1)
				itemGroup = append(itemGroup, normKey)
				if !collapsed[normKey] {
					addTreeItems(normal, normKey)
				}
			}
		}

		addProviderGroup("Claude", "[#FF8C00]", "\xf0\x9f\xa7\xa0", claudeSessions, -2)

		if len(claudeSessions) > 0 && len(codexSessions) > 0 {
			sessionList.AddItem("", "", 0, nil)
			filteredIdx = append(filteredIdx, -1)
			itemGroup = append(itemGroup, "")
		}

		addProviderGroup("Codex", "[#4A9EFF]", "\xf0\x9f\xa4\x96", codexSessions, -3)
		if len(filteredIdx) > 0 {
			sessionList.SetCurrentItem(0)
			showSessionInfo(filteredIdx[0])
		}
		if len(filteredIdx) == 0 {
			infoView.SetText("[gray]검색 결과 없음[-]")
			convView.SetText("")
		}
		if filter == "" {
			statusBar.SetText(defaultStatus())
		} else {
			statusBar.SetText(fmt.Sprintf(
				"[green]%d/%d개 세션[-] | [yellow]Esc[-] 취소 | [yellow]Enter[-] 열기 | [yellow]i[-] 요약",
				len(filteredIdx), len(sessions),
			))
		}
	}
	populateList("")

	sessionList.SetChangedFunc(func(idx int, _, _ string, _ rune) {
		if previewOpen && idx >= 0 && idx < len(filteredIdx) && filteredIdx[idx] >= 0 {
			showSessionInfo(filteredIdx[idx])
		}
	})

	// ── Actions ──

	newSession := func(provider Provider) {
		home, _ := os.UserHomeDir()
		var cmd, label string
		if provider == ProviderCodex {
			cmd = "codex --sandbox danger-full-access"
			label = "Codex"
		} else {
			cmd = "claude --dangerously-skip-permissions"
			label = "Claude"
		}
		err := openInTerminal(cmd, home, true, app)
		if err != nil {
			statusBar.SetText(fmt.Sprintf("[red]실패: %v[-]", err))
		} else {
			statusBar.SetText(fmt.Sprintf("[green]새 %s 세션[-]", label))
			go func() {
				time.Sleep(2 * time.Second)
				fresh := discoverSessions()
				app.QueueUpdateDraw(func() {
					sessions = fresh
					aliases = loadAliases()
					populateList(currentFilter)
				})
			}()
		}
	}

	openSession := func(idx int, inTab bool) {
		if idx < 0 || idx >= len(filteredIdx) {
			return
		}
		fi := filteredIdx[idx]
		// Handle "New Session" items
		if fi == -2 {
			newSession(ProviderClaude)
			return
		}
		if fi == -3 {
			newSession(ProviderCodex)
			return
		}
		if fi < 0 {
			return
		}
		s := sessions[filteredIdx[idx]]
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
					populateList(currentFilter)
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
		idx := sessionList.GetCurrentItem()
		if idx < 0 || idx >= len(filteredIdx) || filteredIdx[idx] < 0 {
			return
		}
		s := sessions[filteredIdx[idx]]
		if _, ok := summaryCache[s.ID]; ok {
			showSessionInfo(filteredIdx[idx])
			return
		}
		sessionID := s.ID
		sessionIdx := filteredIdx[idx]
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
				curIdx := sessionList.GetCurrentItem()
				if curIdx >= 0 && curIdx < len(filteredIdx) && filteredIdx[curIdx] == sessionIdx {
					showSessionInfo(sessionIdx)
				}
				statusBar.SetText(fmt.Sprintf("[green]요약 완료: %s[-]", esc(s.ProjectName)))
			})
		}()
	}

	sessionList.SetSelectedFunc(func(idx int, _, _ string, _ rune) {
		openSession(idx, true)
	})

	// ── Focus & search ──

	focusables := []tview.Primitive{sessionList, convView}
	focusIdx := 0
	searching := false

	updateBorders := func() {
		if focusIdx == 0 {
			sessionList.SetBorderColor(tcell.ColorGreen)
			convView.SetBorderColor(tcell.ColorDodgerBlue)
		} else {
			sessionList.SetBorderColor(tcell.ColorDodgerBlue)
			convView.SetBorderColor(tcell.ColorGreen)
		}
	}

	showSearch := func() {
		searching = true
		leftPane.Clear()
		leftPane.AddItem(searchInput, 1, 0, false)
		leftPane.AddItem(sessionList, 0, 1, false)
		app.SetFocus(searchInput)
	}

	hideSearch := func() {
		searching = false
		leftPane.Clear()
		leftPane.AddItem(sessionList, 0, 1, true)
		searchInput.SetText("")
		currentFilter = ""
		populateList("")
		focusIdx = 0
		app.SetFocus(sessionList)
		updateBorders()
	}

	searchInput.SetDoneFunc(func(key tcell.Key) {
		if key == tcell.KeyEscape {
			hideSearch()
		} else if key == tcell.KeyEnter {
			focusIdx = 0
			app.SetFocus(sessionList)
			updateBorders()
		}
	})

	searchInput.SetChangedFunc(func(text string) {
		currentFilter = text
		populateList(text)
	})

	// ── Key bindings ──

	app.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		// Skip key mapping when typing in any input field
		if _, ok := app.GetFocus().(*tview.InputField); ok {
			return ev
		}

		switch ev.Key() {
		case tcell.KeyLeft: // Collapse group
			idx := sessionList.GetCurrentItem()
			if idx >= 0 && idx < len(itemGroup) && itemGroup[idx] != "" {
				grp := itemGroup[idx]
				if !collapsed[grp] {
					collapsed[grp] = true
					populateList(currentFilter)
				}
			}
			return nil
		case tcell.KeyRight: // Expand group
			idx := sessionList.GetCurrentItem()
			if idx >= 0 && idx < len(itemGroup) && itemGroup[idx] != "" {
				grp := itemGroup[idx]
				if collapsed[grp] {
					collapsed[grp] = false
					populateList(currentFilter)
				}
			}
			return nil
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
				case 'q':
					app.Stop()
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
							"  d=삭제       D=일괄삭제   o=폴더      /=검색\n" +
							"  r=새로고침   Esc=종료     ?=도움말\n\n" +
							"[gray]아무 키나 누르면 닫힘[-]",
					)
					helpText.SetBorder(true).
						SetTitle(" 도움말 ").
						SetTitleAlign(tview.AlignCenter).
						SetBorderColor(tcell.ColorDodgerBlue).
						SetBackgroundColor(tcell.ColorDarkSlateGray)
					helpText.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
						app.SetRoot(mainLayout, true)
						app.SetFocus(sessionList)
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
					idx := sessionList.GetCurrentItem()
					if idx >= 0 && idx < len(filteredIdx) && filteredIdx[idx] >= 0 {
						s := sessions[filteredIdx[idx]]
						if s.Pinned {
							s.Pinned = false
							delete(localPins, s.ID)
							statusBar.SetText(fmt.Sprintf("[yellow]고정 해제: %s[-]", esc(s.ProjectName)))
						} else {
							s.Pinned = true
							localPins[s.ID] = true
							statusBar.SetText(fmt.Sprintf("[green]📌 고정: %s[-]", esc(s.ProjectName)))
						}
						savePins(localPins)
						sessions = discoverSessions()
						populateList(currentFilter)
					}
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
							populateList(currentFilter)
						})
					}()
					return nil

				case ' ': // Toggle multi-select
					idx := sessionList.GetCurrentItem()
					if idx >= 0 && idx < len(filteredIdx) && filteredIdx[idx] >= 0 {
						s := sessions[filteredIdx[idx]]
						s.Selected = !s.Selected
						populateList(currentFilter)
						sessionList.SetCurrentItem(idx)
						showSessionInfo(filteredIdx[idx])
					}
					return nil

				case 'm': // Rename (alias)
					idx := sessionList.GetCurrentItem()
					if idx < 0 || idx >= len(filteredIdx) || filteredIdx[idx] < 0 {
						return nil
					}
					s := sessions[filteredIdx[idx]]
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
							populateList(currentFilter)
							if newAlias != "" {
								statusBar.SetText(fmt.Sprintf("[green]이름 변경: %s[-]", esc(newAlias)))
							} else {
								statusBar.SetText("[green]별칭 삭제됨[-]")
							}
						}
						app.SetRoot(mainLayout, true)
						app.SetFocus(sessionList)
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

				case 'd': // Delete single session
					idx := sessionList.GetCurrentItem()
					if idx < 0 || idx >= len(filteredIdx) || filteredIdx[idx] < 0 {
						return nil
					}
					s := sessions[filteredIdx[idx]]
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
									populateList(currentFilter)
									statusBar.SetText(fmt.Sprintf("[green]삭제됨: %s[-]", esc(displayName)))
								}
							}
							app.SetRoot(mainLayout, true)
							app.SetFocus(sessionList)
						})
					confirmModal.SetBackgroundColor(tcell.ColorDarkSlateGray)
					confirmModal.SetButtonBackgroundColor(tcell.ColorDimGray)
					confirmModal.SetButtonTextColor(tcell.ColorWhite)
					confirmModal.SetFocus(0) // focus on Delete button
					// Style: focused button = bright, unfocused = dim
					confirmModal.SetButtonActivatedStyle(tcell.StyleDefault.Background(tcell.ColorRed).Foreground(tcell.ColorWhite))
					app.SetRoot(confirmModal, true)
					return nil

				case 'D': // Delete all selected sessions
					var selected []*Session
					for _, s := range sessions {
						if s.Selected {
							selected = append(selected, s)
						}
					}
					if len(selected) == 0 {
						statusBar.SetText("[yellow]선택된 세션 없음. Space로 선택하세요.[-]")
						return nil
					}
					confirmModal := tview.NewModal().
						SetText(fmt.Sprintf("%d개 세션을 삭제하시겠습니까?", len(selected))).
						AddButtons([]string{"전체 삭제", "취소"}).
						SetDoneFunc(func(_ int, label string) {
							if label == "전체 삭제" {
								deleted := 0
								for _, s := range selected {
									if deleteSession(s) == nil {
										delete(aliases, s.ID)
										deleted++
									}
								}
								saveAliases(aliases)
								sessions = discoverSessions()
								populateList(currentFilter)
								statusBar.SetText(fmt.Sprintf("[green]%d개 세션 삭제됨[-]", deleted))
							}
							app.SetRoot(mainLayout, true)
							app.SetFocus(sessionList)
						})
					confirmModal.SetBackgroundColor(tcell.ColorDarkSlateGray)
					confirmModal.SetButtonBackgroundColor(tcell.NewRGBColor(40, 40, 40))
					confirmModal.SetButtonTextColor(tcell.ColorGray)
					confirmModal.SetFocus(0)
					confirmModal.SetButtonActivatedStyle(tcell.StyleDefault.Background(tcell.ColorRed).Foreground(tcell.ColorWhite).Bold(true))
					app.SetRoot(confirmModal, true)
					return nil

				case 'o': // Open session folder in Finder
					idx := sessionList.GetCurrentItem()
					if idx < 0 || idx >= len(filteredIdx) || filteredIdx[idx] < 0 {
						return nil
					}
					s := sessions[filteredIdx[idx]]
					dir := filepath.Dir(s.SessionFile)
					if err := exec.Command("open", dir).Run(); err != nil {
						statusBar.SetText(fmt.Sprintf("[red]폴더 열기 실패: %v[-]", err))
					} else {
						statusBar.SetText(fmt.Sprintf("[green]폴더 열림: %s[-]", esc(dir)))
					}
					return nil
				}
			}
		}
		return ev
	})

	// Background polling: auto-refresh every 10 seconds
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			fresh := discoverSessions()
			app.QueueUpdateDraw(func() {
				sessions = fresh
				aliases = loadAliases()
				populateList(currentFilter)
			})
		}
	}()

	if err := app.SetRoot(mainLayout, true).Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
