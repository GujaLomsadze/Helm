// Package fleet materializes the event stream into live session state.
package fleet

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"hash/fnv"
	"path/filepath"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"github.com/GujaLomsadze/trk/internal/agent"
	"github.com/GujaLomsadze/trk/internal/derive"
	"github.com/GujaLomsadze/trk/internal/gitinfo"
	"github.com/GujaLomsadze/trk/internal/model"
)

type GitLookup func(cwd string) gitinfo.Info

const (
	clockSkew   = 10 * 60 * 1000 // client ts further than this from server now → use server now
	gitCacheTTL = 30 * 1000
)

type entry struct {
	S          model.Session
	T          derive.Tracker
	statusHash uint64
	subSeen    map[string]bool // subagent ids already counted (SubagentStop can repeat)
	restoredAt int64           // last event already folded into the restored session; replays up to it are not re-counted
	deferred   bool            // adapter asked to keep the card hidden until the session does something
}

type gitCached struct {
	info gitinfo.Info
	at   int64
}

// DefaultHideAfter: idle/done cards leave the dashboard after this long without activity.
const DefaultHideAfter int64 = 5 * 3600 * 1000

var (
	ErrNotFound  = errors.New("no such session")
	ErrActive    = errors.New("only idle or done sessions can be dismissed")
	ErrTitleLong = errors.New("title is too long")
)

type Fleet struct {
	mu        sync.Mutex
	hideAfter int64 // ms; 0 = never auto-hide
	sessions  map[string]*entry
	pids      map[int]string // agent PID → session_id (agents with Caps.PIDAttribution)
	account   model.Account
	git       GitLookup
	gitCache  map[string]gitCached
	// OnLearn is called (under the fleet lock) when a Claude pid → session mapping is new or changed,
	// so the daemon can persist it. Must not call back into the fleet.
	OnLearn func(pid int, sessionID string)
}

func New(git GitLookup) *Fleet {
	return &Fleet{sessions: map[string]*entry{}, pids: map[int]string{}, git: git, gitCache: map[string]gitCached{}, hideAfter: DefaultHideAfter}
}

func (f *Fleet) Restore(sessions []model.Session, acct model.Account) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, s := range sessions {
		f.sessions[s.SessionID] = &entry{S: s, restoredAt: s.LastEventAt}
	}
	f.account = acct
}

// ClaudePIDs returns a copy of the known Claude pid → session mappings.
func (f *Fleet) ClaudePIDs() map[int]string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make(map[int]string, len(f.pids))
	for pid, sid := range f.pids {
		out[pid] = sid
	}
	return out
}

// RestorePIDs reloads persisted Claude pid → session mappings after a restart.
func (f *Fleet) RestorePIDs(m map[int]string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for pid, sid := range m {
		f.pids[pid] = sid
	}
}

func (f *Fleet) SetHideAfter(ms int64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if ms < 0 {
		ms = 0
	}
	f.hideAfter = ms
}

func (f *Fleet) HideAfter() int64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.hideAfter
}

// Dismiss hides an idle or done session until it shows new activity.
func (f *Fleet) Dismiss(id string, now int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	e, ok := f.sessions[id]
	if !ok {
		return ErrNotFound
	}
	if st, _ := derive.Status(&e.T, now); !derive.Quiet(st) {
		return ErrActive
	}
	e.S.DismissedAt = now
	return nil
}

// MaxTitle is the longest card title, in characters.
const MaxTitle = 80

// CleanTitle drops control characters and collapses runs of space; too long is an error.
func CleanTitle(title string) (string, error) {
	title = strings.Join(strings.Fields(strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, title)), " ")
	if utf8.RuneCountInString(title) > MaxTitle {
		return "", ErrTitleLong
	}
	return title, nil
}

// SetTitle gives a session a chosen card title; an empty one goes back to the automatic name.
func (f *Fleet) SetTitle(id, title string) error {
	title, err := CleanTitle(title)
	if err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	e, ok := f.sessions[id]
	if !ok {
		return ErrNotFound
	}
	e.S.Title = title
	return nil
}

// SessionRow returns the stored row for a session (e.g. to persist it after Dismiss).
func (f *Fleet) SessionRow(id string) (model.Session, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	e, ok := f.sessions[id]
	if !ok {
		return model.Session{}, false
	}
	return e.S, true
}

func (f *Fleet) Account() model.Account {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.account
}

// Normalize turns an envelope into a stored event. Hooks go through their agent's adapter
// first, so everything after this point reads the canonical schema. Only hook and status
// events say which agent sent them; CLI/API progress reports never set a session's agent.
func (f *Fleet) Normalize(env model.Envelope, now int64) (model.Event, bool) {
	ts := env.TS
	if ts <= 0 || ts > now+clockSkew || ts < now-clockSkew {
		ts = now
	}
	raw := env.Payload
	if len(raw) == 0 || !json.Valid(raw) {
		raw = json.RawMessage(`{}`)
	}

	source := env.Source
	switch source {
	case model.SourceCLI, model.SourceHook, model.SourceStatusline, model.SourceAPI:
	default:
		source = model.SourceAPI
	}
	kind, name, native, key := env.Kind, "", "", ""
	switch source {
	case model.SourceHook:
		name = env.Agent
		if name == "" {
			name = agent.Default // hooks installed before TRK knew other agents
		}
		a, ok := agent.Get(name)
		if !ok {
			return model.Event{}, false
		}
		n, ok := a.Normalize(raw)
		if !ok {
			return model.Event{}, false
		}
		raw, kind, native, key = n.Payload, n.Kind, n.Native, n.Key
		raw = flag(raw, "trk_background", n.Background)
		raw = flag(raw, "trk_defer", n.Defer)
	case model.SourceStatusline:
		kind, name = model.KindStatus, env.Agent
		if name == "" {
			name = agent.Default
		}
	}
	if kind == "" {
		kind = model.KindOther
	}
	raw = trimPayload(raw)
	p := parse(raw)
	if source == model.SourceStatusline {
		native = p.SessionID
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	sid, attr := f.resolve(env, source, name, native, p, now)
	if kind == model.KindStatus {
		if e := f.sessions[sid]; e != nil && e.statusHash == statusKey(p) {
			return model.Event{}, false
		}
	}
	if attr != model.AttrNative {
		native = ""
	}
	if native == "" && attr == model.AttrNative {
		_, native = agent.Split(sid)
	}
	return model.Event{Agent: name, NativeID: native, Key: key, TS: ts, SessionID: sid, Source: source, Kind: kind, Payload: raw, Attribution: attr}, true
}

// flag sets a TRK-owned boolean in a payload object (only when true: the common case stays byte-identical).
func flag(raw json.RawMessage, k string, on bool) json.RawMessage {
	if !on {
		return raw
	}
	var m map[string]json.RawMessage
	if json.Unmarshal(raw, &m) != nil || m == nil {
		return raw
	}
	m[k] = json.RawMessage("true")
	if b, err := json.Marshal(m); err == nil {
		return b
	}
	return raw
}

// statusKey hashes only the status-line fields TRK uses, so refreshes that change
// nothing but volatile fields (durations, line counts) are dropped as duplicates.
func statusKey(p payload) uint64 {
	b, _ := json.Marshal(struct {
		Cwd    string
		Model  string
		Cost   any
		Ctx    any
		Limits any
	}{p.cwd(), p.modelName(), costKey(p), p.ContextWindow, p.RateLimits})
	return hash(b)
}

// costKey is the cost block minus the durations, which tick on every refresh.
func costKey(p payload) any {
	if p.Cost == nil {
		return nil
	}
	c := *p.Cost
	c.TotalAPIDurationMs, c.TotalDurationMs = nil, nil
	return c
}

func hash(b []byte) uint64 {
	h := fnv.New64a()
	h.Write(b)
	return h.Sum64()
}

func (f *Fleet) resolve(env model.Envelope, source, name, native string, p payload, now int64) (string, string) {
	fromAgent := source == model.SourceHook || source == model.SourceStatusline
	learn := func(sid string) {
		if fromAgent && !agent.CapsOf(name).PIDAttribution {
			return // one process may host many sessions: a pid says nothing
		}
		if env.ClaudePID <= 0 || sid == "" || f.pids[env.ClaudePID] == sid {
			return
		}
		f.pids[env.ClaudePID] = sid
		if f.OnLearn != nil {
			f.OnLearn(env.ClaudePID, sid)
		}
	}
	if fromAgent && native != "" {
		sid := agent.SessionKey(name, native)
		learn(sid)
		return sid, model.AttrNative
	}
	if env.SessionID != "" {
		if env.SessionVia == model.AttrEnv {
			return env.SessionID, model.AttrEnv
		}
		learn(env.SessionID)
		return env.SessionID, model.AttrNative
	}
	if sid, ok := f.pids[env.ClaudePID]; ok && env.ClaudePID > 0 && (!fromAgent || agent.CapsOf(name).PIDAttribution) {
		return sid, model.AttrPtree
	}
	cwd := p.cwd()
	if sid := f.matchCwd(cwd, now); sid != "" {
		return sid, model.AttrCwd
	}
	if cwd == "" {
		return "unknown", model.AttrCwd
	}
	sum := sha1.Sum([]byte(filepath.Clean(cwd)))
	return "cwd-" + hex.EncodeToString(sum[:])[:8], model.AttrCwd
}

// matchCwd picks the most plausible live session for a working directory. Among agents
// with Caps.CwdBestMatch: exact cwd > caller in a subdirectory, then same git branch, then
// most recent. Other agents' sessions count only as the single live session in that exact
// folder: a guess between two of their chats would report progress to the wrong one.
func (f *Fleet) matchCwd(cwd string, now int64) string {
	if cwd == "" {
		return ""
	}
	cwd = filepath.Clean(cwd)
	branch := f.gitInfo(cwd, now).Branch
	best, bestScore, bestLast := "", 0, int64(0)
	only, others := "", 0
	for sid, e := range f.sessions {
		if e.S.Cwd == "" || e.S.Background || e.deferred {
			continue
		}
		if st, _ := derive.Status(&e.T, now); st == derive.StatusDone || st == derive.StatusInterrupted {
			continue
		}
		if e.S.Agent != "" && !agent.CapsOf(e.S.Agent).CwdBestMatch {
			if filepath.Clean(e.S.Cwd) == cwd {
				only, others = sid, others+1
			}
			continue
		}
		sc := filepath.Clean(e.S.Cwd)
		score := 0
		switch {
		case sc == cwd:
			score = 4
		case strings.HasPrefix(cwd, sc+string(filepath.Separator)):
			score = 2
		default:
			continue
		}
		if branch != "" && e.S.Branch == branch {
			score++
		}
		if score > bestScore || (score == bestScore && e.S.LastEventAt > bestLast) {
			best, bestScore, bestLast = sid, score, e.S.LastEventAt
		}
	}
	if best == "" && others == 1 {
		return only
	}
	return best
}

func (f *Fleet) gitInfo(cwd string, now int64) gitinfo.Info {
	if c, ok := f.gitCache[cwd]; ok && now-c.at < gitCacheTTL {
		return c.info
	}
	info := f.git(cwd)
	f.gitCache[cwd] = gitCached{info, now}
	return info
}

func (f *Fleet) Apply(ev model.Event, now int64) model.Session {
	f.mu.Lock()
	defer f.mu.Unlock()
	p := parse(ev.Payload)
	e := f.sessions[ev.SessionID]
	if e == nil {
		e = &entry{S: model.Session{SessionID: ev.SessionID, StartedAt: ev.TS}}
		f.sessions[ev.SessionID] = e
	}
	s, t := &e.S, &e.T
	if ev.Agent != "" {
		s.Agent = ev.Agent
	}
	if ev.NativeID != "" {
		s.NativeID = ev.NativeID
	}
	caps := agent.CapsOf(s.Agent)
	if p.Background {
		s.Background = true // sticky: the agent's own work never becomes a user's session
	}
	if p.Defer && s.LastPrompt == "" && s.Task == "" && len(t.Calls) == 0 {
		e.deferred = true
	}
	switch ev.Kind {
	case model.KindPrompt, model.KindStart, model.KindToolPre, model.KindToolPost, model.KindToolFail:
		e.deferred = false
	}
	// Hooks run async: a start or prompt older than the turn's end must not reopen it.
	ended := max(t.DoneAt, t.Interrupted)
	stale := ended > 0 && ev.TS < ended
	if ev.TS > s.LastEventAt {
		s.LastEventAt = ev.TS
	}
	if s.StartedAt == 0 || ev.TS < s.StartedAt {
		s.StartedAt = ev.TS
	}
	if cwd := p.cwd(); cwd != "" && cwd != s.Cwd {
		s.Cwd = cwd
		gi := f.gitInfo(cwd, now)
		s.Repo, s.Branch = gi.Repo, gi.Branch
		s.Name = filepath.Base(filepath.Clean(cwd))
	}
	if model.IsActivity(ev.Kind) {
		t.Activity(ev.TS)
	}

	switch ev.Kind {
	case model.KindSessionStart:
		if m := p.modelName(); m != "" {
			s.Model = m
		}
		if stale {
			break
		}
		t.DoneAt, t.Interrupted = 0, 0
		t.ClearPending(ev.TS)
	case model.KindStart:
		s.Task, s.StepN, s.StepI, s.StepText = p.Text, p.Steps, 0, ""
		t.DoneAt, t.Blocked, t.ProgressAt = 0, nil, ev.TS
	case model.KindStep:
		s.StepText = p.Text
		t.Blocked, t.ProgressAt = nil, ev.TS
	case model.KindProgress:
		s.StepI = p.I
		if p.N > 0 {
			s.StepN = p.N
		}
		t.Blocked, t.ProgressAt = nil, ev.TS
	case model.KindBlocked:
		t.Blocked = &derive.Need{Type: "blocked", Text: p.Text, TS: ev.TS}
	case model.KindDone:
		t.Blocked = nil
		if ev.TS > t.DoneAt {
			t.DoneAt = ev.TS
		}
		if p.Text != "" {
			s.StepText = p.Text
		}
		if s.StepN > 0 {
			s.StepI = s.StepN
		}
	case model.KindPrompt:
		if txt := cardText(p.Prompt, false); txt != "" && ev.TS >= s.LastPromptAt {
			s.LastPrompt, s.LastPromptAt = txt, ev.TS
		}
		if stale {
			break
		}
		t.ClearPending(ev.TS)
		t.Blocked, t.DoneAt, t.Interrupted = nil, 0, 0
	case model.KindSubagentStop:
		// An untyped agent finishing after the turn ended is Claude Code's own
		// background agent, not one the session spawned.
		internal := p.AgentType == "" && t.DoneAt > 0
		if ev.TS > e.restoredAt && !internal { // the restored count already includes older ones
			e.countSubagent(p)
		}
	case model.KindToolPre:
		t.ToolStart(f.call(p, s.Cwd, ev.TS))
		if caps.StopEndsTools && stale {
			t.FinishCalls(ended) // arrived after its turn ended
		}
	case model.KindToolPost, model.KindToolFail:
		c := f.call(p, s.Cwd, 0)
		c.End, c.Failed = ev.TS, ev.Kind == model.KindToolFail
		c.Unknown = !c.Failed && p.ToolOutcome == "unknown"
		t.ToolEnd(c)
		t.ClearPending(ev.TS)
	case model.KindPermission:
		sum, _, _, _ := derive.Describe(p.ToolName, p.ToolInput, s.Cwd)
		text := p.ToolName
		if sum != "" {
			text += ": " + sum
		}
		t.SetPending(derive.Need{Type: "permission", Text: text, TS: ev.TS})
	case model.KindNotification:
		switch p.NotificationType {
		case "permission_prompt":
			t.SetPending(derive.Need{Type: "permission", Text: p.Message, TS: ev.TS, Weak: true})
		case "elicitation_dialog":
			t.SetPending(derive.Need{Type: "question", Text: p.Message, TS: ev.TS, Weak: true})
		}
	case model.KindStop, model.KindSessionEnd:
		if ev.TS > t.DoneAt {
			t.DoneAt = ev.TS
		}
		t.ClearPending(ev.TS)
		if caps.StopEndsTools || ev.Kind == model.KindSessionEnd {
			t.FinishCalls(ev.TS)
		}
		if ev.Kind == model.KindStop {
			if txt := cardText(p.LastAssistant, true); txt != "" {
				s.LastReply, s.LastReplyAt = txt, ev.TS
			}
			if p.BackgroundTasks != nil {
				s.BgRunning = 0
				for _, b := range p.BackgroundTasks {
					if b.Status == "running" {
						s.BgRunning++
					}
				}
			}
		}
	case model.KindInterrupt:
		if ev.TS > t.Interrupted {
			t.Interrupted = ev.TS
		}
		t.ClearPending(ev.TS)
		t.Blocked = nil
		t.FinishCalls(ev.TS)
	case model.KindStatus:
		e.statusHash = statusKey(p)
		f.applyStatus(s, p, ev.TS)
	}
	t.Prune(now)
	s.Status, _ = derive.Status(t, now)
	return *s
}

func (f *Fleet) call(p payload, cwd string, start int64) derive.ToolCall {
	sum, key, file, edit := derive.Describe(p.ToolName, p.ToolInput, cwd)
	c := derive.ToolCall{ID: p.ToolUseID, Tool: p.ToolName, Summary: sum, Key: key, File: file, Edit: edit, Start: start, Kind: p.ToolKind}
	if files := derive.ResolveFiles(p.ToolFiles, cwd); len(files) > 0 {
		c.File, c.Files = files[0], files
		rels := make([]string, len(files))
		for i, f := range files {
			rels[i] = derive.Rel(f, cwd)
		}
		c.Summary = derive.Trunc(strings.Join(rels, ", "))
	}
	if c.Kind == "edit" {
		c.Edit = true
	}
	return c
}

func (f *Fleet) applyStatus(s *model.Session, p payload, ts int64) {
	if m := p.modelName(); m != "" {
		s.Model = m
	}
	if cw := p.ContextWindow; cw != nil {
		if cw.UsedPercentage != nil {
			v := *cw.UsedPercentage
			s.CtxPct = &v
		}
		if cw.ContextWindowSize != nil {
			s.CtxSize = int64(*cw.ContextWindowSize)
		}
		if cw.TotalInputTokens != nil {
			s.TokensIn = int64(*cw.TotalInputTokens)
		}
		if cw.TotalOutputTokens != nil {
			s.TokensOut = int64(*cw.TotalOutputTokens)
		}
	}
	if c := p.Cost; c != nil {
		setNum := func(v *float64, dst *int64) {
			if v != nil {
				*dst = int64(*v)
			}
		}
		if c.TotalCostUSD != nil {
			s.CostUSD = *c.TotalCostUSD
		}
		setNum(c.TotalLinesAdded, &s.LinesAdded)
		setNum(c.TotalLinesRemoved, &s.LinesRemoved)
		setNum(c.TotalAPIDurationMs, &s.APIMs)
		setNum(c.TotalDurationMs, &s.WallMs)
	}
	rl := p.RateLimits
	if rl == nil || ts < f.account.UpdatedAt {
		return
	}
	set := func(w *window, pct **float64, reset *int64) bool {
		if w == nil || w.UsedPercentage == nil {
			return false
		}
		v := *w.UsedPercentage
		*pct = &v
		if w.ResetsAt != nil {
			*reset = int64(*w.ResetsAt) * 1000
		}
		return true
	}
	a := set(rl.FiveHour, &f.account.FiveHPct, &f.account.FiveHReset)
	b := set(rl.SevenDay, &f.account.SevenDPct, &f.account.SevenDReset)
	if a || b {
		f.account.UpdatedAt = ts
	}
}
