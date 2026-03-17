package agent

import (
	"sync"

	"github.com/dmora/crucible/internal/csync"
)

// UsageDelta holds unpersisted token/cost deltas from one or more sources.
type UsageDelta struct {
	InputTokens      int64
	OutputTokens     int64
	ThinkingTokens   int64
	CacheReadTokens  int64
	CacheWriteTokens int64
	CostUSD          float64
}

// TotalTokens returns the total token volume across all categories.
func (d UsageDelta) TotalTokens() int64 {
	return d.InputTokens + d.OutputTokens + d.ThinkingTokens + d.CacheReadTokens + d.CacheWriteTokens
}

// UsageLedger accumulates unpersisted token/cost deltas for a session.
// Deltas are added by stations and the supervisor, then drained (read + clear)
// when persisted to the DB. The DB is the source of truth for cumulative totals.
type UsageLedger struct {
	mu    sync.Mutex
	delta UsageDelta
}

// NewUsageLedger creates a new empty ledger.
func NewUsageLedger() *UsageLedger {
	return &UsageLedger{}
}

// Add accumulates token and cost deltas. Thread-safe.
func (l *UsageLedger) Add(input, output, thinking, cacheRead, cacheWrite int64, cost float64) {
	l.mu.Lock()
	l.delta.InputTokens += input
	l.delta.OutputTokens += output
	l.delta.ThinkingTokens += thinking
	l.delta.CacheReadTokens += cacheRead
	l.delta.CacheWriteTokens += cacheWrite
	l.delta.CostUSD += cost
	l.mu.Unlock()
}

// Drain atomically reads and clears the unpersisted delta.
func (l *UsageLedger) Drain() UsageDelta {
	l.mu.Lock()
	d := l.delta
	l.delta = UsageDelta{}
	l.mu.Unlock()
	return d
}

// Peek reads the current delta without clearing. Thread-safe.
func (l *UsageLedger) Peek() UsageDelta {
	l.mu.Lock()
	d := l.delta
	l.mu.Unlock()
	return d
}

// Session-scoped ledger map (same pattern as processStates).
var usageLedgers = csync.NewMap[string, *UsageLedger]()

// GetOrCreateLedger returns the usage ledger for a session, creating one if needed.
// Uses atomic GetOrSet to avoid races between concurrent callers.
func GetOrCreateLedger(sessionID string) *UsageLedger {
	return usageLedgers.GetOrSet(sessionID, NewUsageLedger)
}

// PurgeLedger removes the usage ledger for a session.
func PurgeLedger(sessionID string) {
	usageLedgers.Del(sessionID)
}
