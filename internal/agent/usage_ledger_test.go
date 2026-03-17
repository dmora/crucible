package agent

import (
	"sync"
	"testing"
)

func TestUsageLedger_AddAndDrain(t *testing.T) {
	l := NewUsageLedger()
	l.Add(100, 50, 10, 20, 5, 0.05)
	l.Add(200, 100, 20, 0, 0, 0.10)

	d := l.Drain()
	if d.InputTokens != 300 {
		t.Errorf("InputTokens = %d, want 300", d.InputTokens)
	}
	if d.OutputTokens != 150 {
		t.Errorf("OutputTokens = %d, want 150", d.OutputTokens)
	}
	if d.ThinkingTokens != 30 {
		t.Errorf("ThinkingTokens = %d, want 30", d.ThinkingTokens)
	}
	if d.CacheReadTokens != 20 {
		t.Errorf("CacheReadTokens = %d, want 20", d.CacheReadTokens)
	}
	if d.CacheWriteTokens != 5 {
		t.Errorf("CacheWriteTokens = %d, want 5", d.CacheWriteTokens)
	}
	if d.TotalTokens() != 505 { // 300+150+30+20+5
		t.Errorf("TotalTokens = %d, want 505", d.TotalTokens())
	}
	wantCost := 0.15
	if d.CostUSD < wantCost-0.001 || d.CostUSD > wantCost+0.001 {
		t.Errorf("CostUSD = %f, want %f", d.CostUSD, wantCost)
	}

	// Drain should have cleared.
	d2 := l.Drain()
	if d2.TotalTokens() != 0 || d2.CostUSD != 0 {
		t.Errorf("After drain, expected zero delta, got tokens=%d cost=%f", d2.TotalTokens(), d2.CostUSD)
	}
}

func TestUsageLedger_DrainEmpty(t *testing.T) {
	l := NewUsageLedger()
	d := l.Drain()
	if d.TotalTokens() != 0 || d.CostUSD != 0 {
		t.Errorf("Empty drain should return zero, got tokens=%d cost=%f", d.TotalTokens(), d.CostUSD)
	}
}

func TestUsageLedger_AddAfterDrain(t *testing.T) {
	l := NewUsageLedger()
	l.Add(100, 50, 0, 0, 0, 0.01)
	l.Drain()
	l.Add(200, 100, 0, 0, 0, 0.02)

	d := l.Drain()
	if d.InputTokens != 200 {
		t.Errorf("InputTokens = %d, want 200", d.InputTokens)
	}
	if d.OutputTokens != 100 {
		t.Errorf("OutputTokens = %d, want 100", d.OutputTokens)
	}
}

func TestUsageLedger_PeekDoesNotClear(t *testing.T) {
	l := NewUsageLedger()
	l.Add(100, 50, 10, 0, 0, 0.05)

	p := l.Peek()
	if p.InputTokens != 100 {
		t.Errorf("Peek InputTokens = %d, want 100", p.InputTokens)
	}

	// Peek again — should return same values.
	p2 := l.Peek()
	if p2.InputTokens != 100 {
		t.Errorf("Second Peek InputTokens = %d, want 100", p2.InputTokens)
	}

	// Drain should also return the same values.
	d := l.Drain()
	if d.InputTokens != 100 {
		t.Errorf("Drain after Peek InputTokens = %d, want 100", d.InputTokens)
	}
}

func TestUsageLedger_ConcurrentAdd(t *testing.T) {
	l := NewUsageLedger()
	const goroutines = 100
	const addsPerGoroutine = 1000

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for range goroutines {
		go func() {
			defer wg.Done()
			for range addsPerGoroutine {
				l.Add(1, 1, 1, 0, 0, 0.001)
			}
		}()
	}
	wg.Wait()

	d := l.Drain()
	want := int64(goroutines * addsPerGoroutine)
	if d.InputTokens != want {
		t.Errorf("InputTokens = %d, want %d", d.InputTokens, want)
	}
	if d.OutputTokens != want {
		t.Errorf("OutputTokens = %d, want %d", d.OutputTokens, want)
	}
	if d.ThinkingTokens != want {
		t.Errorf("ThinkingTokens = %d, want %d", d.ThinkingTokens, want)
	}
	if d.TotalTokens() != want*3 {
		t.Errorf("TotalTokens = %d, want %d", d.TotalTokens(), want*3)
	}
}

func TestUsageDelta_TotalTokens(t *testing.T) {
	d := UsageDelta{InputTokens: 100, OutputTokens: 50, ThinkingTokens: 25, CacheReadTokens: 10, CacheWriteTokens: 5}
	if d.TotalTokens() != 190 { // 100+50+25+10+5
		t.Errorf("TotalTokens = %d, want 190", d.TotalTokens())
	}
}

func TestGetOrCreateLedger(t *testing.T) {
	// Clean up after test.
	defer PurgeLedger("test-session-ledger")

	l1 := GetOrCreateLedger("test-session-ledger")
	l2 := GetOrCreateLedger("test-session-ledger")
	if l1 != l2 {
		t.Error("GetOrCreateLedger should return the same instance for the same session")
	}

	l1.Add(100, 0, 0, 0, 0, 0)
	p := l2.Peek()
	if p.InputTokens != 100 {
		t.Errorf("Shared ledger InputTokens = %d, want 100", p.InputTokens)
	}
}

func TestPurgeLedger(t *testing.T) {
	l := GetOrCreateLedger("test-purge-ledger")
	l.Add(100, 0, 0, 0, 0, 0)
	PurgeLedger("test-purge-ledger")

	l2 := GetOrCreateLedger("test-purge-ledger")
	p := l2.Peek()
	if p.InputTokens != 0 {
		t.Errorf("After purge, InputTokens = %d, want 0", p.InputTokens)
	}
}
