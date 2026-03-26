package prompt

import "testing"

func TestHash_Deterministic(t *testing.T) {
	input := "You are a helpful assistant."
	h1 := Hash(input)
	h2 := Hash(input)
	if h1 != h2 {
		t.Fatalf("Hash is not deterministic: %q != %q", h1, h2)
	}
}

func TestHash_Length(t *testing.T) {
	got := Hash("anything")
	if len(got) != 12 {
		t.Fatalf("expected 12 hex chars, got %d: %q", len(got), got)
	}
}

func TestHash_Uniqueness(t *testing.T) {
	a := Hash("prompt A")
	b := Hash("prompt B")
	if a == b {
		t.Fatalf("different inputs produced same hash: %q", a)
	}
}

func TestHash_Empty(t *testing.T) {
	got := Hash("")
	if len(got) != 12 {
		t.Fatalf("expected 12 hex chars for empty input, got %d: %q", len(got), got)
	}
}
