package agent

import (
	"testing"

	adkmodel "google.golang.org/adk/model"
	adksession "google.golang.org/adk/session"
	"google.golang.org/genai"

	"github.com/dmora/crucible/internal/csync"
	"github.com/dmora/crucible/internal/message"
	"github.com/dmora/crucible/internal/pubsub"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- groundingFromMetadata tests ---

func TestGroundingFromMetadata_DomainExtraction(t *testing.T) {
	gm := &genai.GroundingMetadata{
		WebSearchQueries: []string{"test query"},
		GroundingChunks: []*genai.GroundingChunk{
			{Web: &genai.GroundingChunkWeb{URI: "https://example.com/page1", Title: "Example Page"}},
			{Web: &genai.GroundingChunkWeb{URI: "https://docs.google.com/doc/123", Title: "Google Doc"}},
			{Web: &genai.GroundingChunkWeb{URI: "https://sub.domain.org:8080/path", Title: "Subdomain"}},
		},
	}

	gc := groundingFromMetadata(gm)

	require.Len(t, gc.Sources, 3)
	assert.Equal(t, "example.com", gc.Sources[0].Domain)
	assert.Equal(t, "Example Page", gc.Sources[0].Title)
	assert.Equal(t, "https://example.com/page1", gc.Sources[0].URL)

	assert.Equal(t, "docs.google.com", gc.Sources[1].Domain)
	assert.Equal(t, "sub.domain.org", gc.Sources[2].Domain)
}

func TestGroundingFromMetadata_MalformedURI(t *testing.T) {
	gm := &genai.GroundingMetadata{
		GroundingChunks: []*genai.GroundingChunk{
			{Web: &genai.GroundingChunkWeb{URI: "not-a-valid-url", Title: "Bad URL"}},
		},
	}

	gc := groundingFromMetadata(gm)

	require.Len(t, gc.Sources, 1)
	assert.Equal(t, "not-a-valid-url", gc.Sources[0].Domain, "should fall back to raw URI")
}

func TestGroundingFromMetadata_SupportsPopulated(t *testing.T) {
	gm := &genai.GroundingMetadata{
		GroundingChunks: []*genai.GroundingChunk{
			{Web: &genai.GroundingChunkWeb{URI: "https://a.com", Title: "A"}},
			{Web: &genai.GroundingChunkWeb{URI: "https://b.com", Title: "B"}},
		},
		GroundingSupports: []*genai.GroundingSupport{
			{
				Segment:               &genai.Segment{Text: "claim one"},
				GroundingChunkIndices: []int32{0},
				ConfidenceScores:      []float32{0.9},
			},
			{
				Segment:               &genai.Segment{Text: "claim two"},
				GroundingChunkIndices: []int32{0, 1},
				ConfidenceScores:      []float32{0.8, 0.7},
			},
		},
	}

	gc := groundingFromMetadata(gm)

	require.Len(t, gc.Supports, 2)
	assert.Equal(t, "claim one", gc.Supports[0].Text)
	assert.Equal(t, []int32{0}, gc.Supports[0].ChunkIndices)
	assert.Equal(t, []float32{0.9}, gc.Supports[0].Scores)
	assert.Equal(t, "claim two", gc.Supports[1].Text)
	assert.Equal(t, []int32{0, 1}, gc.Supports[1].ChunkIndices)
}

func TestGroundingFromMetadata_NilSegment(t *testing.T) {
	gm := &genai.GroundingMetadata{
		GroundingSupports: []*genai.GroundingSupport{
			{
				Segment:               nil,
				GroundingChunkIndices: []int32{0},
				ConfidenceScores:      []float32{0.5},
			},
		},
	}

	gc := groundingFromMetadata(gm)

	require.Len(t, gc.Supports, 1)
	assert.Empty(t, gc.Supports[0].Text, "nil segment should produce empty text")
	assert.Equal(t, []int32{0}, gc.Supports[0].ChunkIndices)
}

func TestGroundingFromMetadata_NoSupports(t *testing.T) {
	gm := &genai.GroundingMetadata{
		WebSearchQueries:  []string{"q"},
		GroundingSupports: nil,
	}

	gc := groundingFromMetadata(gm)
	assert.Nil(t, gc.Supports)
}

// --- citationsFromMetadata tests ---

func TestCitationsFromMetadata_Populated(t *testing.T) {
	cm := &genai.CitationMetadata{
		Citations: []*genai.Citation{
			{StartIndex: 0, EndIndex: 50, URI: "https://example.com", Title: "Example", License: "MIT"},
			{StartIndex: 100, EndIndex: 150, URI: "https://other.com", Title: "Other"},
		},
	}

	result := citationsFromMetadata(cm)

	require.Len(t, result, 2)
	assert.Equal(t, int32(0), result[0].StartIndex)
	assert.Equal(t, int32(50), result[0].EndIndex)
	assert.Equal(t, "https://example.com", result[0].URI)
	assert.Equal(t, "MIT", result[0].License)
	assert.Empty(t, result[1].License, "empty license when not set")
}

func TestCitationsFromMetadata_Nil(t *testing.T) {
	assert.Nil(t, citationsFromMetadata(nil))
	assert.Nil(t, citationsFromMetadata(&genai.CitationMetadata{}))
}

// --- handleGrounding / publishGrounding tests ---

func TestHandleGrounding_WithCitations(t *testing.T) {
	broker := pubsub.NewBroker[message.Message]()
	metrics := csync.NewMap[string, *TurnMetrics]()
	var result AgentResult
	msg := newInMemoryMessage("sess-1", message.Assistant, nil, "", "")
	ep := newEventProcessor(broker, metrics, &msg, &result, nil, nil, nil)

	event := &adksession.Event{
		LLMResponse: adkmodel.LLMResponse{
			GroundingMetadata: &genai.GroundingMetadata{
				WebSearchQueries: []string{"test"},
				GroundingChunks: []*genai.GroundingChunk{
					{Web: &genai.GroundingChunkWeb{URI: "https://a.com", Title: "A"}},
				},
			},
			CitationMetadata: &genai.CitationMetadata{
				Citations: []*genai.Citation{
					{StartIndex: 0, EndIndex: 10, URI: "https://a.com", Title: "A"},
				},
			},
		},
	}

	ep.handleGrounding(event)

	gc := msg.Grounding()
	require.NotNil(t, gc)
	assert.Len(t, gc.Sources, 1)
	assert.Len(t, gc.Citations, 1)
	assert.Equal(t, "https://a.com", gc.Citations[0].URI)
}

func TestPublishGrounding_ProgressiveAccumulation(t *testing.T) {
	broker := pubsub.NewBroker[message.Message]()
	metrics := csync.NewMap[string, *TurnMetrics]()
	var result AgentResult
	msg := newInMemoryMessage("sess-1", message.Assistant, nil, "", "")
	ep := newEventProcessor(broker, metrics, &msg, &result, nil, nil, nil)

	// Event 1: queries + chunks, no supports.
	gm1 := &genai.GroundingMetadata{
		WebSearchQueries: []string{"q1"},
		GroundingChunks: []*genai.GroundingChunk{
			{Web: &genai.GroundingChunkWeb{URI: "https://a.com", Title: "A"}},
		},
	}
	ep.publishGrounding(gm1, nil)
	gc := msg.Grounding()
	require.NotNil(t, gc)
	assert.Len(t, gc.Sources, 1)
	assert.Empty(t, gc.Supports)
	assert.Nil(t, gc.Citations)

	// Event 2: same queries + chunks + supports.
	gm2 := &genai.GroundingMetadata{
		WebSearchQueries: []string{"q1"},
		GroundingChunks: []*genai.GroundingChunk{
			{Web: &genai.GroundingChunkWeb{URI: "https://a.com", Title: "A"}},
		},
		GroundingSupports: []*genai.GroundingSupport{
			{Segment: &genai.Segment{Text: "claim"}, GroundingChunkIndices: []int32{0}},
		},
	}
	ep.publishGrounding(gm2, nil)
	gc = msg.Grounding()
	require.NotNil(t, gc)
	assert.Len(t, gc.Sources, 1, "sources should be unchanged")
	assert.Len(t, gc.Supports, 1, "supports should be populated")
	assert.Nil(t, gc.Citations)

	// Event 3: standalone citations.
	cm := &genai.CitationMetadata{
		Citations: []*genai.Citation{
			{StartIndex: 0, EndIndex: 10, URI: "https://a.com", Title: "A"},
		},
	}
	ep.publishGrounding(nil, cm)
	gc = msg.Grounding()
	require.NotNil(t, gc)
	assert.Len(t, gc.Sources, 1, "sources unchanged")
	assert.Len(t, gc.Supports, 1, "supports unchanged")
	assert.Len(t, gc.Citations, 1, "citations now populated")
}

func TestPublishGrounding_StandaloneCitations(t *testing.T) {
	broker := pubsub.NewBroker[message.Message]()
	metrics := csync.NewMap[string, *TurnMetrics]()
	var result AgentResult
	msg := newInMemoryMessage("sess-1", message.Assistant, nil, "", "")
	ep := newEventProcessor(broker, metrics, &msg, &result, nil, nil, nil)

	cm := &genai.CitationMetadata{
		Citations: []*genai.Citation{
			{StartIndex: 0, EndIndex: 20, URI: "https://cite.com", Title: "Cite"},
		},
	}
	ep.publishGrounding(nil, cm)

	gc := msg.Grounding()
	require.NotNil(t, gc, "should create GroundingContent from standalone citations")
	assert.Empty(t, gc.Sources)
	assert.Len(t, gc.Citations, 1)
}

// --- groundingToMessage tests ---

func TestGroundingToMessage_NilGroundingMetadata(t *testing.T) {
	cm := &genai.CitationMetadata{
		Citations: []*genai.Citation{
			{StartIndex: 0, EndIndex: 10, URI: "https://a.com", Title: "A"},
		},
	}
	msg := &message.Message{ID: "m1", Parts: []message.ContentPart{}}

	groundingToMessage(nil, cm, msg)

	gc := msg.Grounding()
	require.NotNil(t, gc)
	assert.Empty(t, gc.Sources)
	assert.Len(t, gc.Citations, 1)
}

// --- eventsToMessages replay tests ---

func TestEventsToMessages_GroundingWithSupports(t *testing.T) {
	userEvent := newEvent("e1", "user", []*genai.Part{{Text: "hello"}})

	agentEvent := newEventWithFinish("e2", "agent",
		[]*genai.Part{{Text: "response"}},
		genai.FinishReasonStop,
	)
	agentEvent.GroundingMetadata = &genai.GroundingMetadata{
		WebSearchQueries: []string{"test"},
		GroundingChunks: []*genai.GroundingChunk{
			{Web: &genai.GroundingChunkWeb{URI: "https://a.com", Title: "A"}},
		},
		GroundingSupports: []*genai.GroundingSupport{
			{Segment: &genai.Segment{Text: "claim"}, GroundingChunkIndices: []int32{0}, ConfidenceScores: []float32{0.9}},
		},
	}
	agentEvent.CitationMetadata = &genai.CitationMetadata{
		Citations: []*genai.Citation{
			{StartIndex: 0, EndIndex: 8, URI: "https://a.com", Title: "A"},
		},
	}

	events := testEvents{userEvent, agentEvent}
	msgs := eventsToMessages(events, "sess-1")

	// Find the assistant message with grounding.
	var assistantMsg *message.Message
	for i := range msgs {
		if msgs[i].Role == message.Assistant {
			assistantMsg = &msgs[i]
			break
		}
	}
	require.NotNil(t, assistantMsg)

	gc := assistantMsg.Grounding()
	require.NotNil(t, gc, "replay should preserve grounding")
	assert.Len(t, gc.Sources, 1)
	assert.Equal(t, "a.com", gc.Sources[0].Domain)
	assert.Len(t, gc.Supports, 1)
	assert.Equal(t, "claim", gc.Supports[0].Text)
	assert.Len(t, gc.Citations, 1)
}
