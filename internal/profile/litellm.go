package profile

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// GatewaySpend is what a LiteLLM proxy reports about the key it was called with.
type GatewaySpend struct {
	SpendUSD  float64  `json:"spend_usd"`
	BudgetUSD *float64 `json:"budget_usd"`         // max_budget; nil = no limit on the key
	ResetAt   int64    `json:"reset_at,omitempty"` // unix ms of the next budget reset
	Alias     string   `json:"alias,omitempty"`
	Checked   int64    `json:"checked"` // unix ms
}

// ErrNotLiteLLM: the gateway answered but not like LiteLLM's /key/info.
var ErrNotLiteLLM = errors.New("the gateway doesn't answer /key/info like LiteLLM")

// HTTPClient is the client spend lookups use; tests replace it.
var HTTPClient = &http.Client{Timeout: 8 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error {
	return http.ErrUseLastResponse // never carry the key to another host
}}

// LiteLLMSpend asks the proxy at base about key. A key may read its own /key/info on
// LiteLLM; some proxies only let the admin key do it, which shows up as ErrForbidden.
// The URL is tried as given and at its root, since the Anthropic route often has a path.
func LiteLLMSpend(ctx context.Context, base, key string, now time.Time) (GatewaySpend, error) {
	u, err := url.Parse(strings.TrimRight(base, "/"))
	if err != nil || u.Host == "" {
		return GatewaySpend{}, errors.New("bad gateway URL")
	}
	tries := []string{u.String() + "/key/info"}
	if strings.Trim(u.Path, "/") != "" {
		root := *u
		root.Path, root.RawQuery = "", ""
		tries = append(tries, root.String()+"/key/info")
	}
	var last error
	for _, t := range tries {
		sp, err := keyInfo(ctx, t, key, now)
		if err == nil {
			return sp, nil
		}
		last = err
		if errors.Is(err, ErrForbidden) {
			break // the proxy is there and said no; its root won't say yes
		}
	}
	return GatewaySpend{}, last
}

// ErrForbidden: the proxy refused to tell the key its own spend.
var ErrForbidden = errors.New("the gateway won't show this key its spend (401/403): ask its admin, or use Helm's estimate")

func keyInfo(ctx context.Context, endpoint, key string, now time.Time) (GatewaySpend, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return GatewaySpend{}, err
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Accept", "application/json")
	res, err := HTTPClient.Do(req)
	if err != nil {
		return GatewaySpend{}, fmt.Errorf("gateway unreachable: %v", redact(err.Error(), key))
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if res.StatusCode == http.StatusUnauthorized || res.StatusCode == http.StatusForbidden {
		return GatewaySpend{}, ErrForbidden
	}
	if res.StatusCode != http.StatusOK {
		return GatewaySpend{}, fmt.Errorf("%w (HTTP %d)", ErrNotLiteLLM, res.StatusCode)
	}
	var v struct {
		Info *struct {
			Spend     *float64 `json:"spend"`
			MaxBudget *float64 `json:"max_budget"`
			ResetAt   string   `json:"budget_reset_at"`
			Alias     string   `json:"key_alias"`
		} `json:"info"`
	}
	if json.Unmarshal(body, &v) != nil || v.Info == nil || v.Info.Spend == nil {
		return GatewaySpend{}, ErrNotLiteLLM
	}
	sp := GatewaySpend{SpendUSD: *v.Info.Spend, BudgetUSD: v.Info.MaxBudget, Alias: v.Info.Alias, Checked: now.UnixMilli()}
	if v.Info.ResetAt != "" {
		for _, layout := range []string{time.RFC3339Nano, "2006-01-02T15:04:05.999999", "2006-01-02 15:04:05.999999"} {
			if t, err := time.Parse(layout, v.Info.ResetAt); err == nil {
				sp.ResetAt = t.UnixMilli()
				break
			}
		}
	}
	return sp, nil
}

func redact(s, key string) string {
	if key == "" {
		return s
	}
	return strings.ReplaceAll(s, key, Mask(key))
}
