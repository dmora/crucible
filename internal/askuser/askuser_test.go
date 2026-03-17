package askuser

import (
	"context"
	"testing"
	"time"
)

func TestRequestBlockingAndRespond(t *testing.T) {
	svc := NewService(false)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Subscribe before making the request to capture the request ID.
	subCh := svc.Subscribe(ctx)

	questions := []Question{{
		ID:       "approach",
		Question: "Which approach?",
		Header:   "Approach",
		Options:  []Option{{Label: "A"}, {Label: "B"}},
	}}

	done := make(chan *Response, 1)
	errCh := make(chan error, 1)
	go func() {
		resp, err := svc.Request(context.Background(), "sess1", "", questions)
		if err != nil {
			errCh <- err
			return
		}
		done <- resp
	}()

	// Wait for the published request.
	var reqID string
	select {
	case event := <-subCh:
		reqID = event.Payload.ID
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for request event")
	}

	// Respond.
	svc.Respond(reqID, Response{
		Answers: []Answer{{
			ID:       "approach",
			Question: "Which approach?",
			Values:   []string{"A"},
		}},
	})

	// Request should unblock.
	select {
	case resp := <-done:
		if resp == nil {
			t.Fatal("expected non-nil response")
		}
		if len(resp.Answers) != 1 {
			t.Fatalf("expected 1 answer, got %d", len(resp.Answers))
		}
		if resp.Answers[0].Values[0] != "A" {
			t.Errorf("expected value 'A', got %q", resp.Answers[0].Values[0])
		}
	case err := <-errCh:
		t.Fatalf("unexpected error: %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for response")
	}
}

func TestRequestCanceled(t *testing.T) {
	svc := NewService(false)

	ctx, cancel := context.WithCancel(context.Background())

	questions := []Question{{
		ID:       "q1",
		Question: "Question?",
		Header:   "Q",
	}}

	done := make(chan error, 1)
	go func() {
		_, err := svc.Request(ctx, "sess1", "", questions)
		done <- err
	}()

	// Give the goroutine time to block.
	time.Sleep(50 * time.Millisecond)

	cancel()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected error on cancellation")
		}
		if !isContextError(err) {
			t.Errorf("expected context error, got: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for cancellation")
	}
}

func isContextError(err error) bool {
	return err == context.Canceled || err == context.DeadlineExceeded
}

func TestRequestNonInteractive(t *testing.T) {
	svc := NewService(true)

	questions := []Question{{
		ID:       "q1",
		Question: "Question?",
		Header:   "Q",
	}}

	_, err := svc.Request(context.Background(), "sess1", "", questions)
	if err != ErrNonInteractive {
		t.Errorf("expected ErrNonInteractive, got: %v", err)
	}
}

func TestRespondUnknownID(t *testing.T) {
	svc := NewService(false)

	// Should not panic.
	svc.Respond("unknown-id", Response{Canceled: true})
}

func TestSerializedRequests(t *testing.T) {
	svc := NewService(false)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	subCh := svc.Subscribe(ctx)

	q1 := []Question{{ID: "q1", Question: "First?", Header: "Q1"}}
	q2 := []Question{{ID: "q2", Question: "Second?", Header: "Q2"}}

	// Start first request.
	resp1Ch := make(chan *Response, 1)
	go func() {
		resp, _ := svc.Request(context.Background(), "sess1", "", q1)
		resp1Ch <- resp
	}()

	// Wait for first request to be published.
	var req1ID string
	select {
	case event := <-subCh:
		req1ID = event.Payload.ID
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for first request")
	}

	// Start second request — should block on the mutex.
	resp2Started := make(chan struct{})
	resp2Ch := make(chan *Response, 1)
	go func() {
		close(resp2Started)
		resp, _ := svc.Request(context.Background(), "sess1", "", q2)
		resp2Ch <- resp
	}()

	<-resp2Started
	time.Sleep(50 * time.Millisecond)

	// Second request should not have been published yet.
	select {
	case <-resp2Ch:
		t.Fatal("second request returned before first was resolved")
	default:
		// expected
	}

	// Respond to first request.
	svc.Respond(req1ID, Response{
		Answers: []Answer{{ID: "q1", Question: "First?", Values: []string{"yes"}}},
	})

	select {
	case <-resp1Ch:
		// first request resolved
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for first response")
	}

	// Now second request should proceed. Wait for its publish.
	var req2ID string
	select {
	case event := <-subCh:
		req2ID = event.Payload.ID
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for second request")
	}

	svc.Respond(req2ID, Response{
		Answers: []Answer{{ID: "q2", Question: "Second?", Values: []string{"no"}}},
	})

	select {
	case resp := <-resp2Ch:
		if resp.Answers[0].Values[0] != "no" {
			t.Errorf("expected 'no', got %q", resp.Answers[0].Values[0])
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for second response")
	}
}

func TestPublishOnRequest(t *testing.T) {
	svc := NewService(false)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	subCh := svc.Subscribe(ctx)

	questions := []Question{{
		ID:       "q1",
		Question: "Question?",
		Header:   "Q",
	}}

	go func() {
		resp, _ := svc.Request(context.Background(), "sess1", "", questions)
		_ = resp
	}()

	select {
	case event := <-subCh:
		if event.Payload.SessionID != "sess1" {
			t.Errorf("expected session 'sess1', got %q", event.Payload.SessionID)
		}
		if len(event.Payload.Questions) != 1 {
			t.Errorf("expected 1 question, got %d", len(event.Payload.Questions))
		}
		// Respond to unblock the goroutine.
		svc.Respond(event.Payload.ID, Response{Canceled: true})
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for published request")
	}
}

func TestIDUniquenessValidation(t *testing.T) {
	svc := NewService(false)

	// Duplicate IDs should be rejected.
	questions := []Question{
		{ID: "same", Question: "Q1?", Header: "Q1"},
		{ID: "same", Question: "Q2?", Header: "Q2"},
	}

	_, err := svc.Request(context.Background(), "sess1", "", questions)
	if err == nil {
		t.Fatal("expected error for duplicate IDs")
	}
}

func TestEmptyIDValidation(t *testing.T) {
	svc := NewService(false)

	questions := []Question{
		{ID: "", Question: "Q1?", Header: "Q1"},
	}

	_, err := svc.Request(context.Background(), "sess1", "", questions)
	if err == nil {
		t.Fatal("expected error for empty ID")
	}
}

func TestCanceledResponse(t *testing.T) {
	svc := NewService(false)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	subCh := svc.Subscribe(ctx)

	questions := []Question{{
		ID:       "q1",
		Question: "Question?",
		Header:   "Q",
	}}

	done := make(chan *Response, 1)
	errCh := make(chan error, 1)
	go func() {
		resp, err := svc.Request(context.Background(), "sess1", "", questions)
		if err != nil {
			errCh <- err
			return
		}
		done <- resp
	}()

	var reqID string
	select {
	case event := <-subCh:
		reqID = event.Payload.ID
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for request event")
	}

	svc.Respond(reqID, Response{Canceled: true})

	select {
	case resp := <-done:
		if !resp.Canceled {
			t.Error("expected Canceled to be true")
		}
	case err := <-errCh:
		t.Fatalf("unexpected error: %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for canceled response")
	}
}

func TestToolCallIDPropagated(t *testing.T) {
	svc := NewService(false)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	subCh := svc.Subscribe(ctx)

	questions := []Question{{
		ID:       "q1",
		Question: "Question?",
		Header:   "Q",
	}}

	go func() {
		resp, _ := svc.Request(context.Background(), "sess1", "fc-42", questions)
		_ = resp
	}()

	select {
	case event := <-subCh:
		if event.Payload.ToolCallID != "fc-42" {
			t.Errorf("expected ToolCallID 'fc-42', got %q", event.Payload.ToolCallID)
		}
		// Respond to unblock the goroutine.
		svc.Respond(event.Payload.ID, Response{Canceled: true})
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for request event")
	}
}

func TestSetNonInteractive(t *testing.T) {
	svc := NewService(false)
	if svc.NonInteractive() {
		t.Error("expected non-interactive to be false initially")
	}
	svc.SetNonInteractive(true)
	if !svc.NonInteractive() {
		t.Error("expected non-interactive to be true after setting")
	}
}

func TestSetNonInteractiveConcurrent(t *testing.T) {
	svc := NewService(false)

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 100; i++ {
			svc.SetNonInteractive(i%2 == 0)
		}
	}()

	for i := 0; i < 100; i++ {
		_ = svc.NonInteractive()
	}

	<-done
}
