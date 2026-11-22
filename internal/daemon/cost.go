package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/GujaLomsadze/helm/internal/model"
	"github.com/GujaLomsadze/helm/internal/profile"
)

// Cost: every status line carries the session's cumulative cost estimate; the growth
// since the previous one goes into the hourly ledger (store.cost_hours). The Limits page
// shows it per launch profile, against each profile's budget. A profile on a LiteLLM
// gateway can show the gateway's own count of the key's spend instead.

const (
	hourMs      = 3600 * 1000
	gatewayTTL  = 60 * 1000 // one /key/info call a minute per profile
	gatewayWait = 8 * time.Second
	maxCostDays = 400
)

type costTracker struct {
	mu   sync.Mutex
	last map[string]float64 // session → latest cumulative cost seen
}

// trackCost adds a status event's cost growth to the ledger.
func (s *Server) trackCost(ev model.Event) {
	if ev.Kind != model.KindStatus || ev.SessionID == "" {
		return
	}
	var p struct {
		Cost struct {
			Total *float64 `json:"total_cost_usd"`
		} `json:"cost"`
	}
	if json.Unmarshal(ev.Payload, &p) != nil || p.Cost.Total == nil {
		return
	}
	cur := *p.Cost.Total
	s.cost.mu.Lock()
	prev, seen := s.cost.last[ev.SessionID]
	if !seen {
		if c, ok, err := s.st.LastCost(ev.SessionID, ev.TS); err == nil && ok {
			prev, seen = c, true
		}
	}
	s.cost.last[ev.SessionID] = cur
	s.cost.mu.Unlock()
	d := cur
	if seen && cur >= prev {
		d = cur - prev
	}
	if err := s.st.AddCost(ev.SessionID, ev.TS, d); err != nil {
		s.log.Printf("cost ledger: %v", err)
	}
}

type gwEntry struct {
	at  int64
	sp  *profile.GatewaySpend
	err string
}

type gatewayCache struct {
	mu sync.Mutex
	m  map[string]gwEntry
}

func (g *gatewayCache) forget(id string) {
	g.mu.Lock()
	delete(g.m, id)
	g.mu.Unlock()
}

// gatewaySpend asks the profile's gateway for the key's spend, at most once a minute.
func (s *Server) gatewaySpend(ctx context.Context, p profile.Profile, sec map[string]string) gwEntry {
	now := s.now()
	s.gw.mu.Lock()
	e, ok := s.gw.m[p.ID]
	s.gw.mu.Unlock()
	if ok && now-e.at < gatewayTTL {
		return e
	}
	base, key, ok := profile.Endpoint(p, sec)
	if !ok {
		e = gwEntry{at: now, err: "the profile has no gateway URL and key"}
	} else {
		sp, err := profile.LiteLLMSpend(ctx, base, key, time.UnixMilli(now))
		e = gwEntry{at: now}
		if err != nil {
			e.err = err.Error()
		} else {
			e.sp = &sp
		}
	}
	s.gw.mu.Lock()
	s.gw.m[p.ID] = e
	s.gw.mu.Unlock()
	return e
}

type costProfile struct {
	ID         string  `json:"id"` // "" = sessions started without a profile
	Name       string  `json:"name"`
	Agent      string  `json:"agent,omitempty"`
	Preset     string  `json:"preset,omitempty"`
	Color      string  `json:"color,omitempty"`
	Deleted    bool    `json:"deleted,omitempty"`
	RangeUSD   float64 `json:"range_usd"` // Helm's estimate over the requested days
	BudgetUSD  float64 `json:"budget_usd,omitempty"`
	BudgetDays int     `json:"budget_days,omitempty"`
	// WindowUSD is Helm's estimate over the budget window (rolling BudgetDays, or since
	// the profile was created), the "spent" a local budget compares.
	WindowUSD   float64               `json:"window_usd"`
	WindowStart int64                 `json:"window_start,omitempty"`
	Spend       string                `json:"spend,omitempty"`
	Gateway     *profile.GatewaySpend `json:"gateway,omitempty"`
	GatewayErr  string                `json:"gateway_error,omitempty"`
	Sessions    int                   `json:"sessions"` // with spend in the range
}

type costPoint struct {
	Profile string  `json:"profile"`
	Hour    int64   `json:"hour"`
	USD     float64 `json:"usd"`
}

// getCost: GET /v1/cost?days=30 → spend per profile per hour over the last days, and each
// profile's budget state.
func (s *Server) getCost(w http.ResponseWriter, r *http.Request) {
	days := 30
	if v := r.URL.Query().Get("days"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > maxCostDays {
			http.Error(w, "days: 1 to 400", http.StatusBadRequest)
			return
		}
		days = n
	}
	now := s.now()
	since := (now - int64(days)*dayMs) / hourMs * hourMs

	pubs := s.publicProfiles()
	from := since
	for _, p := range pubs {
		if st := windowStart(p.Profile, now); st < from {
			from = st
		}
	}
	rows, err := s.st.CostHours(from)
	if err != nil {
		http.Error(w, "store error", http.StatusInternalServerError)
		return
	}
	tags := s.sessionProfiles(nil)

	byID := map[string]*costProfile{"": {ID: "", Name: "No profile"}}
	order := []string{""}
	for _, p := range pubs {
		byID[p.ID] = &costProfile{ID: p.ID, Name: p.Name, Agent: p.Agent, Preset: p.Preset, Color: p.Color,
			BudgetUSD: p.BudgetUSD, BudgetDays: p.BudgetDays, Spend: p.Spend, WindowStart: windowStart(p.Profile, now)}
		order = append(order, p.ID)
	}
	series := map[[2]any]float64{}
	seen := map[string]map[string]bool{}
	for _, c := range rows {
		pid := tags[c.Session]
		cp, ok := byID[pid]
		if !ok {
			cp = &costProfile{ID: pid, Name: "Deleted profile", Deleted: true}
			byID[pid] = cp
			order = append(order, pid)
		}
		if cp.WindowStart > 0 && c.Hour >= cp.WindowStart/hourMs*hourMs {
			cp.WindowUSD += c.USD
		}
		if c.Hour < since {
			continue
		}
		cp.RangeUSD += c.USD
		series[[2]any{pid, c.Hour}] += c.USD
		if seen[pid] == nil {
			seen[pid] = map[string]bool{}
		}
		seen[pid][c.Session] = true
	}

	// Gateways answer in parallel; a slow one doesn't hold up the others.
	ctx, cancel := context.WithTimeout(r.Context(), gatewayWait)
	defer cancel()
	var wg sync.WaitGroup
	for _, p := range pubs {
		if p.Spend != profile.SpendLiteLLM {
			continue
		}
		prof, sec, ok := s.profileByID(p.ID)
		if !ok {
			continue
		}
		cp := byID[p.ID]
		wg.Add(1)
		go func() {
			defer wg.Done()
			e := s.gatewaySpend(ctx, prof, sec)
			cp.Gateway, cp.GatewayErr = e.sp, e.err
		}()
	}
	wg.Wait()

	out := struct {
		Now      int64         `json:"now"`
		Since    int64         `json:"since"`
		Days     int           `json:"days"`
		Profiles []costProfile `json:"profiles"`
		Series   []costPoint   `json:"series"`
	}{Now: now, Since: since, Days: days, Profiles: []costProfile{}, Series: []costPoint{}}
	for _, id := range order {
		cp := byID[id]
		cp.Sessions = len(seen[id])
		if id == "" && cp.RangeUSD == 0 && len(pubs) > 0 {
			continue // no unprofiled spend to show
		}
		out.Profiles = append(out.Profiles, *cp)
	}
	for k, usd := range series {
		out.Series = append(out.Series, costPoint{Profile: k[0].(string), Hour: k[1].(int64), USD: usd})
	}
	writeJSON(w, out)
}

// windowStart is where a profile's budget window begins: BudgetDays back, or its creation.
func windowStart(p profile.Profile, now int64) int64 {
	if p.BudgetDays > 0 {
		return now - int64(p.BudgetDays)*dayMs
	}
	return p.Created
}

// probeGateway: POST /v1/profiles/probe {"url","key","id"} → whether the URL answers like
// a LiteLLM proxy for that key, and what it reports. With "id" and no "key" it uses the
// profile's stored key, so the dashboard can check without ever holding it.
func (s *Server) probeGateway(w http.ResponseWriter, r *http.Request) {
	var in struct {
		URL string `json:"url"`
		Key string `json:"key"`
		ID  string `json:"id"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&in); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	if in.Key == "" && in.ID != "" {
		if p, sec, ok := s.profileByID(in.ID); ok {
			if base, key, ok := profile.Endpoint(p, sec); ok {
				in.Key = key
				if in.URL == "" {
					in.URL = base
				}
			}
		}
	}
	if in.URL == "" || in.Key == "" {
		http.Error(w, "want a gateway URL and a key", http.StatusBadRequest)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), gatewayWait)
	defer cancel()
	sp, err := profile.LiteLLMSpend(ctx, in.URL, in.Key, time.UnixMilli(s.now()))
	res := map[string]any{"litellm": err == nil}
	if err == nil {
		res["spend"] = sp
	} else {
		res["error"] = err.Error()
		res["forbidden"] = errors.Is(err, profile.ErrForbidden)
	}
	writeJSON(w, res)
}
