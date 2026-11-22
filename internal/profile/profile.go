// Package profile holds launch profiles: named ways to start an agent against a model
// provider (a company gateway, an API key, a cloud account), each a preset from the
// agent's adapter plus the values the user filled in. Keys live apart from the rest,
// in a file only the user can read, and never leave the daemon once saved.
package profile

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/GujaLomsadze/helm/internal/agent"
)

// Spend sources: where a profile's budget reads its spend from.
const (
	SpendLocal   = "local"   // Helm adds up the agent's own cost estimates
	SpendLiteLLM = "litellm" // the gateway's /key/info, which knows the real spend
)

const (
	MaxProfiles = 50
	maxName     = 40
	maxValue    = 4096
	maxExtra    = 30
)

var (
	envName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,127}$`)
	idRe    = regexp.MustCompile(`^[a-f0-9]{12}$`)
	colorRe = regexp.MustCompile(`^#[0-9a-f]{6}$`)
)

// Profile is what is stored and edited. Values holds the preset's non-secret fields and
// Extra any additional variables; secrets are in the Secrets store, keyed by ID.
type Profile struct {
	ID         string            `json:"id"`
	Agent      string            `json:"agent"`
	Name       string            `json:"name"`
	Preset     string            `json:"preset"`
	Values     map[string]string `json:"values,omitempty"`
	Extra      map[string]string `json:"extra,omitempty"`
	Color      string            `json:"color,omitempty"`
	BudgetUSD  float64           `json:"budget_usd,omitempty"`  // 0 = no budget
	BudgetDays int               `json:"budget_days,omitempty"` // rolling window; 0 = since created
	Spend      string            `json:"spend,omitempty"`       // SpendLocal (default) or SpendLiteLLM
	Created    int64             `json:"created"`
}

// Public is a profile as the dashboard sees it: which secrets are set, never their values.
type Public struct {
	Profile
	Secrets map[string]bool `json:"secrets"` // var → set
}

// Input is a create or update request. A secret left out (or null) keeps the stored one;
// "" clears it.
type Input struct {
	Agent      string             `json:"agent"`
	Name       string             `json:"name"`
	Preset     string             `json:"preset"`
	Values     map[string]string  `json:"values"`
	Secrets    map[string]*string `json:"secrets"`
	Extra      map[string]string  `json:"extra"`
	Color      string             `json:"color"`
	BudgetUSD  float64            `json:"budget_usd"`
	BudgetDays int                `json:"budget_days"`
	Spend      string             `json:"spend"`
}

func NewID() string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func ValidID(id string) bool { return idRe.MatchString(id) }

// PresetOf finds the preset a profile names on its agent.
func PresetOf(agentName, id string) (agent.Preset, agent.Profiler, error) {
	ad, ok := agent.Get(agentName)
	if !ok {
		return agent.Preset{}, nil, errors.New("unknown agent")
	}
	pr, ok := ad.(agent.Profiler)
	if !ok {
		return agent.Preset{}, nil, fmt.Errorf("%s can't use launch profiles", ad.Label())
	}
	for _, p := range pr.Presets() {
		if p.ID == id {
			return p, pr, nil
		}
	}
	return agent.Preset{}, nil, errors.New("unknown preset")
}

// Apply validates in against its preset and returns the profile to store (prev is the
// stored one when updating, nil when creating) and the secrets it should have.
func Apply(prev *Profile, prevSecrets map[string]string, in Input) (Profile, map[string]string, error) {
	var p Profile
	if prev != nil {
		p = *prev
		in.Agent = prev.Agent // a profile can't move to another agent
	}
	if in.Agent == "" {
		in.Agent = agent.Default
	}
	ps, _, err := PresetOf(in.Agent, in.Preset)
	if err != nil {
		return p, nil, err
	}
	name := strings.TrimSpace(in.Name)
	if name == "" || len([]rune(name)) > maxName || strings.ContainsAny(name, "\n\r\t") {
		return p, nil, fmt.Errorf("name: 1 to %d characters on one line", maxName)
	}
	p.Agent, p.Name, p.Preset = in.Agent, name, ps.ID
	p.Color = strings.ToLower(strings.TrimSpace(in.Color))
	if p.Color != "" && !colorRe.MatchString(p.Color) {
		return p, nil, errors.New("color: #rrggbb")
	}
	if in.BudgetUSD < 0 || in.BudgetUSD > 1e7 || math.IsNaN(in.BudgetUSD) || in.BudgetDays < 0 || in.BudgetDays > 3660 {
		return p, nil, errors.New("budget: 0 to 10,000,000 USD over 0 to 3660 days")
	}
	p.BudgetUSD, p.BudgetDays = in.BudgetUSD, in.BudgetDays
	switch in.Spend {
	case "", SpendLocal:
		p.Spend = SpendLocal
	case SpendLiteLLM:
		if ps.Spend != SpendLiteLLM {
			return p, nil, errors.New("this preset has no gateway to ask for spend")
		}
		p.Spend = SpendLiteLLM
	default:
		return p, nil, errors.New("spend: local or litellm")
	}

	p.Values = map[string]string{}
	secrets := map[string]string{}
	for _, f := range ps.Fields {
		if f.Secret {
			v, given := in.Secrets[f.Var]
			switch {
			case given && v != nil:
				if s := strings.TrimSpace(*v); s != "" {
					secrets[f.Var] = s
				}
			case prevSecrets[f.Var] != "":
				secrets[f.Var] = prevSecrets[f.Var]
			}
			if f.Required && secrets[f.Var] == "" {
				return p, nil, fmt.Errorf("%s is required", f.Label)
			}
			if err := checkValue(f.Label, secrets[f.Var]); err != nil {
				return p, nil, err
			}
			continue
		}
		v := strings.TrimSpace(in.Values[f.Var])
		if v == "" {
			v = f.Default
		}
		if f.Required && v == "" {
			return p, nil, fmt.Errorf("%s is required", f.Label)
		}
		if err := checkValue(f.Label, v); err != nil {
			return p, nil, err
		}
		if f.URL && v != "" {
			if u, err := url.Parse(v); err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
				return p, nil, fmt.Errorf("%s: want an http(s) URL", f.Label)
			}
		}
		if v != "" {
			p.Values[f.Var] = v
		}
	}
	if len(in.Extra) > maxExtra {
		return p, nil, fmt.Errorf("at most %d extra variables", maxExtra)
	}
	p.Extra = map[string]string{}
	for k, v := range in.Extra {
		k = strings.TrimSpace(k)
		if !envName.MatchString(k) {
			return p, nil, fmt.Errorf("extra variable %q: letters, digits and _ only", k)
		}
		if err := checkValue(k, v); err != nil {
			return p, nil, err
		}
		p.Extra[k] = v
	}
	return p, secrets, nil
}

func checkValue(label, v string) error {
	if len(v) > maxValue || strings.ContainsRune(v, 0) {
		return fmt.Errorf("%s: too long", label)
	}
	return nil
}

// Env is what a terminal started with p gets on top of the scrubbed environment: the
// preset's fixed variables, the user's values, the secrets, then the extra variables
// (they win, so anything can be overridden).
func Env(p Profile, secrets map[string]string) ([]string, error) {
	ps, _, err := PresetOf(p.Agent, p.Preset)
	if err != nil {
		return nil, err
	}
	m := map[string]string{}
	for k, v := range ps.Fixed {
		m[k] = v
	}
	for _, f := range ps.Fields {
		if f.Secret {
			if v := secrets[f.Var]; v != "" {
				m[f.Var] = v
			}
		} else if v := p.Values[f.Var]; v != "" {
			m[f.Var] = v
		}
	}
	for k, v := range p.Extra {
		m[k] = v
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		out = append(out, k+"="+m[k])
	}
	return out, nil
}

// Endpoint is the profile's gateway URL and the key that goes with it, for asking the
// gateway about spend. ok=false when the preset has no URL field or nothing is set.
func Endpoint(p Profile, secrets map[string]string) (base, key string, ok bool) {
	ps, _, err := PresetOf(p.Agent, p.Preset)
	if err != nil {
		return "", "", false
	}
	for _, f := range ps.Fields {
		if f.URL {
			base = p.Values[f.Var]
		}
		if f.Secret && key == "" {
			key = secrets[f.Var]
		}
	}
	return base, key, base != "" && key != ""
}

// Mask shows the end of a key so the user can tell keys apart: "…a1b2".
func Mask(s string) string {
	if len(s) <= 8 {
		return "…"
	}
	return "…" + s[len(s)-4:]
}

// Secrets keeps every profile's keys in one JSON file, mode 0600. An empty path keeps
// them in memory only (tests).
type Secrets struct {
	path string
	mu   sync.Mutex
	m    map[string]map[string]string // profile id → var → value
}

func OpenSecrets(path string) (*Secrets, error) {
	s := &Secrets{path: path, m: map[string]map[string]string{}}
	if path == "" {
		return s, nil
	}
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(b, &s.m); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if s.m == nil {
		s.m = map[string]map[string]string{}
	}
	return s, nil
}

func (s *Secrets) Get(id string) map[string]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[string]string{}
	for k, v := range s.m[id] {
		out[k] = v
	}
	return out
}

// Set replaces a profile's secrets (nil or empty removes them) and saves the file.
func (s *Secrets) Set(id string, vals map[string]string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	old, had := s.m[id]
	if len(vals) == 0 {
		delete(s.m, id)
	} else {
		cp := map[string]string{}
		for k, v := range vals {
			cp[k] = v
		}
		s.m[id] = cp
	}
	if err := s.save(); err != nil {
		if had {
			s.m[id] = old
		} else {
			delete(s.m, id)
		}
		return err
	}
	return nil
}

func (s *Secrets) save() error {
	if s.path == "" {
		return nil
	}
	b, err := json.MarshalIndent(s.m, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.path), ".secrets-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), s.path)
}
