// Package market discovers active Up/Down hourly markets via the Gamma API.
// Mirror of Python market_finder.py.
package market

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/gipsh/polymarket-bot-go/internal/config"
	"github.com/gipsh/polymarket-bot-go/internal/types"
)

// ── Time zone constants ───────────────────────────────────────────────────

const etLocation = "America/New_York"

// etSlots maps time-slot label → hour of day (24h, ET).
var etSlots = map[string]int{
	"12am": 0, "1am": 1, "2am": 2, "3am": 3, "4am": 4, "5am": 5,
	"6am": 6, "7am": 7, "8am": 8, "9am": 9, "10am": 10, "11am": 11,
	"12pm": 12, "1pm": 13, "2pm": 14, "3pm": 15, "4pm": 16, "5pm": 17,
	"6pm": 18, "7pm": 19, "8pm": 20, "9pm": 21, "10pm": 22, "11pm": 23,
}

var monthNames = []string{
	"", "january", "february", "march", "april", "may", "june",
	"july", "august", "september", "october", "november", "december",
}

// allAssetSlugs maps known asset tickers to Polymarket slug prefixes.
var allAssetSlugs = map[string]string{
	"BTC": "bitcoin",
	"ETH": "ethereum",
	"SOL": "solana",
	"XRP": "xrp",
}

// Finder discovers active Up/Down hourly markets for configured assets.
type Finder struct {
	gammaURL  string
	assets    map[string]string // ticker → slug prefix (filtered by config.Assets)
	httpCli   *http.Client
}

// NewFinder creates a Finder for the configured assets.
func NewFinder() *Finder {
	// Filter to only configured assets
	active := map[string]string{}
	for ticker, slug := range allAssetSlugs {
		for _, a := range config.Assets {
			if a == slug {
				active[ticker] = slug
				break
			}
		}
	}

	return &Finder{
		gammaURL: config.GammaHost + "/markets",
		assets:   active,
		httpCli:  &http.Client{Timeout: 10 * time.Second},
	}
}

// GetActiveMarkets returns all open markets closing within MaxMarketAgeH hours,
// sorted by time-to-close (soonest first).
func (f *Finder) GetActiveMarkets() ([]*types.Market, error) {
	slugs := f.buildCandidateSlugs()

	markets := make([]*types.Market, 0, len(slugs))
	for _, candidate := range slugs {
		m, err := f.fetchMarket(candidate.asset, candidate.slug)
		if err != nil {
			log.Printf("[market] %s: %v", candidate.slug, err)
			continue
		}
		if m != nil && m.IsClosingSoon(config.MaxMarketAgeH) {
			markets = append(markets, m)
		}
	}

	sort.Slice(markets, func(i, j int) bool {
		return markets[i].MinutesToClose() < markets[j].MinutesToClose()
	})

	log.Printf("[market] Found %d active markets (closes within %dh)", len(markets), config.MaxMarketAgeH)
	return markets, nil
}

// ── Candidate slug generation ─────────────────────────────────────────────

type candidate struct{ asset, slug string }

func (f *Finder) buildCandidateSlugs() []candidate {
	etLoc, err := time.LoadLocation(etLocation)
	if err != nil {
		etLoc = time.UTC
	}
	nowET := time.Now().In(etLoc)

	// Check from 2h ago to MaxMarketAgeH+1h ahead
	window := config.MaxMarketAgeH + 3
	seen := map[string]bool{}
	var candidates []candidate

	for hoursAhead := -2; hoursAhead <= window; hoursAhead++ {
		target := nowET.Add(time.Duration(hoursAhead) * time.Hour)

		for slotLabel, slotHour := range etSlots {
			slotDT := time.Date(target.Year(), target.Month(), target.Day(),
				slotHour, 0, 0, 0, etLoc)

			// Slot runs for 2 hours; skip if already closed
			closeDT := slotDT.Add(2 * time.Hour)
			if closeDT.Before(nowET) {
				continue
			}
			// Skip if starts too far in the future
			if slotDT.Sub(nowET) > time.Duration(config.MaxMarketAgeH+1)*time.Hour {
				continue
			}

			month := strings.ToLower(monthNames[int(slotDT.Month())])
			day := fmt.Sprintf("%d", slotDT.Day())

			for ticker, assetSlug := range f.assets {
				slug := fmt.Sprintf("%s-up-or-down-%s-%s-%s-et",
					assetSlug, month, day, slotLabel)
				if !seen[slug] {
					seen[slug] = true
					candidates = append(candidates, candidate{ticker, slug})
				}
			}
		}
	}

	log.Printf("[market] Checking %d candidate slugs", len(candidates))
	return candidates
}

// ── Gamma API fetch ───────────────────────────────────────────────────────

func (f *Finder) fetchMarket(asset, slug string) (*types.Market, error) {
	params := url.Values{}
	params.Set("slug", slug)

	resp, err := f.httpCli.Get(f.gammaURL + "?" + params.Encode())
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	// Response is either []item or {data: []item}
	var items []json.RawMessage
	if err := json.Unmarshal(body, &items); err != nil {
		var wrapped struct {
			Data []json.RawMessage `json:"data"`
		}
		if err2 := json.Unmarshal(body, &wrapped); err2 != nil {
			return nil, nil // not found
		}
		items = wrapped.Data
	}

	if len(items) == 0 {
		return nil, nil // market not found yet
	}

	return parseMarket(asset, slug, items[0])
}

// ── Gamma API item parser ─────────────────────────────────────────────────

type gammaItem struct {
	ConditionID  string          `json:"conditionId"`
	Title        string          `json:"title"`
	EndDate      string          `json:"endDate"`
	EndDateISO   string          `json:"endDateIso"`
	ClobTokenIDs json.RawMessage `json:"clobTokenIds"`
	Tokens       json.RawMessage `json:"tokens"`
}

type gammaToken struct {
	Outcome     string `json:"outcome"`
	TokenID     string `json:"token_id"`
	TokenIDCamel string `json:"tokenId"`
	ClobTokenID  string `json:"clobTokenId"`
}

func parseMarket(asset, slug string, raw json.RawMessage) (*types.Market, error) {
	var item gammaItem
	if err := json.Unmarshal(raw, &item); err != nil {
		return nil, err
	}

	condID := item.ConditionID
	if condID == "" {
		return nil, nil
	}

	endDateStr := item.EndDate
	if endDateStr == "" {
		endDateStr = item.EndDateISO
	}
	if endDateStr == "" {
		return nil, nil
	}

	endDate, err := time.Parse(time.RFC3339, strings.Replace(endDateStr, "Z", "+00:00", 1))
	if err != nil {
		// try without timezone
		endDate, err = time.Parse("2006-01-02T15:04:05", endDateStr)
		if err != nil {
			return nil, fmt.Errorf("parse endDate %q: %w", endDateStr, err)
		}
		endDate = endDate.UTC()
	}

	// Parse token IDs ─ try structured tokens first, then clobTokenIds
	upID, downID := extractTokenIDs(item)
	if upID == "" || downID == "" {
		return nil, nil
	}

	return &types.Market{
		Asset:       asset,
		Slug:        slug,
		ConditionID: condID,
		UpTokenID:   upID,
		DownTokenID: downID,
		EndDate:     endDate,
		Title:       item.Title,
	}, nil
}

func extractTokenIDs(item gammaItem) (upID, downID string) {
	// 1. Try structured tokens list
	if item.Tokens != nil {
		var tokens []gammaToken
		if err := json.Unmarshal(item.Tokens, &tokens); err == nil {
			for _, t := range tokens {
				tid := t.TokenID
				if tid == "" {
					tid = t.TokenIDCamel
				}
				if tid == "" {
					tid = t.ClobTokenID
				}
				switch strings.ToLower(t.Outcome) {
				case "up":
					upID = tid
				case "down":
					downID = tid
				}
			}
		}
	}

	// 2. Fallback: clobTokenIds[0]=Up, [1]=Down
	if (upID == "" || downID == "") && item.ClobTokenIDs != nil {
		var ids []string
		if err := json.Unmarshal(item.ClobTokenIDs, &ids); err == nil {
			if len(ids) >= 1 && upID == "" {
				upID = ids[0]
			}
			if len(ids) >= 2 && downID == "" {
				downID = ids[1]
			}
		}
	}

	return
}
