// Package term runs agents in pseudo-terminals for the dashboard's agent drawer: each
// terminal is a Claude or Codex process whose output is kept in a capped scrollback
// and fanned out to any number of attached viewers.
package term

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/GujaLomsadze/trk/internal/codex"
)

const (
	scrollbackMax = 256 << 10 // bytes replayed to a viewer that attaches late
	MaxRunning    = 16        // live terminals at once; each one is a whole claude
)

// ErrUnsupported: this platform has no usable pseudo-terminal support.
var ErrUnsupported = errors.New("terminals are not supported on this platform")

// Spec says what to run. Resume continues an existing conversation by session id;
// Fork (with Resume) continues a copy of it under a new id, leaving the original alone.
type Spec struct {
	Batch    bool   `json:"-"` // scheduled one-shot run; interactive API callers cannot set it
	Provider string `json:"provider,omitempty"`
	Dir      string `json:"dir"`
	Prompt   string `json:"prompt"`
	Resume   string `json:"resume,omitempty"`
	Fork     bool   `json:"fork,omitempty"`
	Title    string `json:"title,omitempty"` // card title for the session it starts (the daemon applies it)
}

var sessionID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,127}$`)

// Info is what the dashboard lists.
type Info struct {
	Provider string `json:"provider"`
	ID       string `json:"id"`
	Dir      string `json:"dir"`
	Name     string `json:"name"`
	Prompt   string `json:"prompt,omitempty"`
	Resume   string `json:"resume,omitempty"` // session id it was started to resume or fork
	Fork     bool   `json:"fork,omitempty"`
	Title    string `json:"title,omitempty"`
	// SessionID is the Claude session running in it, once its hooks have reported one
	// (the daemon links it by process id). Kept after the process exits.
	SessionID string `json:"session_id,omitempty"`
	Started   int64  `json:"started"` // unix ms
	PID       int    `json:"pid"`
	Exited    bool   `json:"exited"`
	ExitCode  *int   `json:"exit_code,omitempty"`
}

type Manager struct {
	Command      string // program to run; "claude" unless a test swaps it
	CodexCommand string // optional executable override, primarily for tests
	OnChange     func() // called (without locks held) when a terminal starts, exits or is removed
	mu           sync.Mutex
	startMu      sync.Mutex // reserve the running limit across concurrent launches
	terms        map[string]*Term
}

func NewManager() *Manager { return &Manager{Command: "claude", terms: map[string]*Term{}} }

func (m *Manager) Start(sp Spec) (*Term, error) {
	m.startMu.Lock()
	defer m.startMu.Unlock()
	if sp.Provider == "" {
		sp.Provider = "claude"
	}
	if sp.Provider != "claude" && sp.Provider != "codex" {
		return nil, errors.New("unknown agent provider")
	}
	dir := filepath.Clean(sp.Dir)
	if sp.Dir == "" || !filepath.IsAbs(dir) {
		return nil, errors.New("folder must be an absolute path")
	}
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		return nil, errors.New("folder does not exist")
	}
	if sp.Resume != "" && !sessionID.MatchString(sp.Resume) {
		return nil, errors.New("bad session id")
	}
	if sp.Fork && sp.Resume == "" {
		return nil, errors.New("fork needs a session to fork")
	}
	if m.Running() >= MaxRunning {
		return nil, fmt.Errorf("%d agents are already running in TRK; stop one first", MaxRunning)
	}
	command := m.Command
	if sp.Provider == "codex" {
		command = m.CodexCommand
		if command == "" {
			var err error
			command, err = codex.Executable()
			if err != nil {
				return nil, err
			}
		}
	}
	id := newID()
	cmd := exec.Command(command, args(sp)...)
	cmd.Dir = dir
	cmd.Env = append(CleanEnv(), "TERM=xterm-256color", "COLORTERM=truecolor", "TRK_TERMINAL_ID="+id)
	f, err := startPty(cmd, 120, 32)
	if errors.Is(err, exec.ErrNotFound) {
		return nil, fmt.Errorf("%s is not on TRK's PATH; start TRK from a shell where `%s` works (trk stop, then trk open)", command, command)
	}
	if err != nil {
		return nil, err
	}
	t := &Term{
		info: Info{ID: id, Provider: sp.Provider, Dir: dir, Name: filepath.Base(dir), Prompt: strings.TrimSpace(sp.Prompt),
			Resume: sp.Resume, Fork: sp.Fork, Title: sp.Title, Started: time.Now().UnixMilli(), PID: f.Pid()},
		f: f, subs: map[chan []byte]struct{}{}, ring: ring{max: scrollbackMax},
	}
	if sp.Resume != "" && !sp.Fork {
		t.info.SessionID = sp.SessionKey() // a fork gets its new id from its first hook event
	}
	m.mu.Lock()
	m.terms[t.info.ID] = t
	m.mu.Unlock()
	go t.pump(m.changed)
	m.changed()
	return t, nil
}

// args builds provider-specific argv; prompts never become shell expressions.
func args(sp Spec) []string {
	if sp.Provider == "codex" {
		a := []string{"--no-daemon"}
		if sp.Batch {
			return append(a, "exec", "--color", "never", "--", strings.TrimSpace(sp.Prompt))
		}
		if sp.Resume != "" {
			if sp.Fork {
				a = append(a, "fork")
			} else {
				a = append(a, "resume")
			}
			a = append(a, sp.Resume)
		}
		if p := strings.TrimSpace(sp.Prompt); p != "" {
			a = append(a, "--", p)
		}
		return a
	}
	var a []string
	if sp.Batch {
		return []string{"--print", "--", strings.TrimSpace(sp.Prompt)}
	}
	if sp.Resume != "" {
		a = append(a, "--resume", sp.Resume)
		if sp.Fork {
			a = append(a, "--fork-session")
		}
	}
	if p := strings.TrimSpace(sp.Prompt); p != "" {
		a = append(a, p)
	}
	return a
}

func (sp Spec) SessionKey() string {
	if sp.Resume == "" {
		return ""
	}
	if sp.Provider == "codex" {
		return "codex:" + sp.Resume
	}
	return sp.Resume
}

// LinkCodex binds only the first root session; subagents cannot replace it.
func (m *Manager) LinkCodex(id, sid string) bool {
	t, ok := m.Get(id)
	if !ok || !strings.HasPrefix(sid, "codex:") {
		return false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.info.Exited || t.info.Provider != "codex" || t.info.SessionID != "" {
		return false
	}
	t.info.SessionID = sid
	return true
}

// Link records which Claude session runs in terminal id. It reports whether that changed.
func (m *Manager) Link(id, sessionID string) bool {
	t, ok := m.Get(id)
	if !ok || sessionID == "" {
		return false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.info.SessionID == sessionID {
		return false
	}
	t.info.SessionID = sessionID
	return true
}

func (m *Manager) Get(id string) (*Term, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.terms[id]
	return t, ok
}

// List returns all terminals, oldest first, including ones whose process exited.
func (m *Manager) List() []Info {
	m.mu.Lock()
	out := make([]Info, 0, len(m.terms))
	for _, t := range m.terms {
		out = append(out, t.Info())
	}
	m.mu.Unlock()
	sort.Slice(out, func(i, j int) bool { return out[i].Started < out[j].Started })
	return out
}

// Running counts terminals whose process is still alive.
func (m *Manager) Running() int {
	n := 0
	for _, i := range m.List() {
		if !i.Exited {
			n++
		}
	}
	return n
}

// Stop kills the terminal's process (if alive) and forgets it.
func (m *Manager) Stop(id string) error {
	m.mu.Lock()
	t, ok := m.terms[id]
	delete(m.terms, id)
	m.mu.Unlock()
	if !ok {
		return errors.New("no such terminal")
	}
	t.kill()
	m.changed()
	return nil
}

// DropExited forgets exited terminals that ran sessionID, except keep.
func (m *Manager) DropExited(sessionID, keep string) {
	m.mu.Lock()
	n := 0
	for id, t := range m.terms {
		if i := t.Info(); id != keep && i.Exited && i.SessionID == sessionID {
			delete(m.terms, id)
			n++
		}
	}
	m.mu.Unlock()
	if n > 0 {
		m.changed()
	}
}

// Close kills every terminal; the daemon calls it on shutdown.
func (m *Manager) Close() {
	m.mu.Lock()
	all := m.terms
	m.terms = map[string]*Term{}
	m.mu.Unlock()
	for _, t := range all {
		t.kill()
	}
}

func (m *Manager) changed() {
	if m.OnChange != nil {
		m.OnChange()
	}
}

type Term struct {
	f    terminalProcess
	mu   sync.Mutex
	info Info
	ring ring
	subs map[chan []byte]struct{}
	done bool
}

func (t *Term) Info() Info {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.info
}

// Output returns a copy of the bounded terminal scrollback for persisted run history.
func (t *Term) Output() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return string(t.ring.bytes())
}

type terminalProcess interface {
	io.ReadWriteCloser
	Pid() int
	Wait() error
	Resize(uint16, uint16) error
}

// Attach returns the scrollback so far and a channel of new output. The channel is
// closed when the process exits or when the viewer falls too far behind (it should
// then re-attach). detach must be called when the viewer goes away.
func (t *Term) Attach() (scrollback []byte, out <-chan []byte, detach func()) {
	ch := make(chan []byte, 256)
	t.mu.Lock()
	defer t.mu.Unlock()
	snap := t.ring.bytes()
	if t.done {
		close(ch)
		return snap, ch, func() {}
	}
	t.subs[ch] = struct{}{}
	return snap, ch, func() {
		t.mu.Lock()
		defer t.mu.Unlock()
		if _, ok := t.subs[ch]; ok {
			delete(t.subs, ch)
			close(ch)
		}
	}
}

func (t *Term) Write(p []byte) error { _, err := t.f.Write(p); return err }

func (t *Term) Resize(cols, rows uint16) error {
	if cols == 0 || rows == 0 || cols > 32767 || rows > 32767 {
		return errors.New("invalid terminal dimensions")
	}
	return t.f.Resize(cols, rows)
}

// pump copies pty output into the scrollback and to every viewer until the process ends.
func (t *Term) pump(changed func()) {
	buf := make([]byte, 32<<10)
	for {
		n, err := t.f.Read(buf)
		if n > 0 {
			b := append([]byte(nil), buf[:n]...)
			t.mu.Lock()
			t.ring.write(b)
			for ch := range t.subs {
				select {
				case ch <- b:
				default: // viewer stuck: drop it; it re-attaches and replays the scrollback
					delete(t.subs, ch)
					close(ch)
				}
			}
			t.mu.Unlock()
		}
		if err != nil { // EOF, or EIO: how Linux reports that the child side closed
			break
		}
	}
	err := t.f.Wait()
	code := 0
	if err != nil {
		code = -1
		if e, ok := err.(interface{ ExitCode() int }); ok {
			code = e.ExitCode()
		}
	}
	_ = t.f.Close()
	t.mu.Lock()
	t.done, t.info.Exited = true, true
	t.info.ExitCode = &code
	for ch := range t.subs {
		close(ch)
	}
	t.subs = map[chan []byte]struct{}{}
	t.mu.Unlock()
	changed()
}

func (t *Term) kill() {
	_ = t.f.Close()
}

// CleanEnv is this process's environment minus the launching Claude session's own
// variables. The daemon is spawned by Claude Code hooks, so it carries CLAUDECODE,
// CLAUDE_CODE_CHILD_SESSION, the session id and its messaging socket/token; a child
// claude that inherits them runs as that session's sub-agent and saves no transcript.
func CleanEnv() []string {
	out := []string{}
	for _, kv := range os.Environ() {
		k, _, _ := strings.Cut(kv, "=")
		k = strings.ToUpper(k)
		if k == "CLAUDECODE" || k == "CLAUDE_PID" || k == "CLAUDE_EFFORT" || strings.HasPrefix(k, "CLAUDE_CODE_") || k == "CODEX_THREAD_ID" || k == "CODEX_SESSION_ID" || k == "CODEX_APP_TOOLS_PIPE_PATH" || k == "CODEX_INTERNAL_ORIGINATOR_OVERRIDE" || k == "TRK_SESSION" || k == "TRK_TERMINAL_ID" || k == "TERM" || k == "COLORTERM" {
			continue
		}
		out = append(out, kv)
	}
	return out
}

// ring keeps the last max bytes written.
type ring struct {
	max int
	b   []byte
}

func (r *ring) write(p []byte) {
	r.b = append(r.b, p...)
	if over := len(r.b) - r.max; over > 0 {
		r.b = append(r.b[:0], r.b[over:]...)
	}
}

func (r *ring) bytes() []byte { return append([]byte(nil), r.b...) }

func newID() string {
	var b [6]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}
