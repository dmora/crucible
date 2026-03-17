package agent

import (
	"context"
	"errors"
	"testing"
	"time"

	adkagent "google.golang.org/adk/agent"
	adkmodel "google.golang.org/adk/model"
	adksession "google.golang.org/adk/session"
	"google.golang.org/genai"

	"github.com/dmora/crucible/internal/csync"
	"github.com/dmora/crucible/internal/message"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- mocks ---

// mockCallbackContext implements agent.CallbackContext for tests.
type mockCallbackContext struct {
	context.Context
	sessionID    string
	invocationID string
	branch       string
}

func (m *mockCallbackContext) SessionID() string                       { return m.sessionID }
func (m *mockCallbackContext) InvocationID() string                    { return m.invocationID }
func (m *mockCallbackContext) Branch() string                          { return m.branch }
func (m *mockCallbackContext) AgentName() string                       { return "test-agent" }
func (m *mockCallbackContext) UserID() string                          { return "user" }
func (m *mockCallbackContext) AppName() string                         { return "crucible" }
func (m *mockCallbackContext) UserContent() *genai.Content             { return nil }
func (m *mockCallbackContext) ReadonlyState() adksession.ReadonlyState { return nil }
func (m *mockCallbackContext) Artifacts() adkagent.Artifacts           { return nil }
func (m *mockCallbackContext) State() adksession.State                 { return nil }

func newMockCtx(sessionID string) *mockCallbackContext {
	return &mockCallbackContext{
		Context:      context.Background(),
		sessionID:    sessionID,
		invocationID: "inv-1",
		branch:       "",
	}
}

func newCanceledMockCtx(sessionID string) *mockCallbackContext {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return &mockCallbackContext{
		Context:      ctx,
		sessionID:    sessionID,
		invocationID: "inv-1",
		branch:       "",
	}
}

// mockSession implements adksession.Session for tests.
type mockSession struct {
	id string
}

func (m *mockSession) ID() string                { return m.id }
func (m *mockSession) AppName() string           { return "crucible" }
func (m *mockSession) UserID() string            { return "user" }
func (m *mockSession) State() adksession.State   { return nil }
func (m *mockSession) Events() adksession.Events { return nil }
func (m *mockSession) LastUpdateTime() time.Time { return time.Time{} }

// mockSessionService records AppendEvent calls and can be configured to fail.
type mockSessionService struct {
	appendedEvents []*adksession.Event
	failOnCall     int // 1-based: fail on the Nth call (0 = never fail)
	callCount      int
}

func (m *mockSessionService) Create(_ context.Context, _ *adksession.CreateRequest) (*adksession.CreateResponse, error) {
	return nil, errors.New("not implemented")
}

func (m *mockSessionService) Get(_ context.Context, _ *adksession.GetRequest) (*adksession.GetResponse, error) {
	return nil, errors.New("not implemented")
}

func (m *mockSessionService) List(_ context.Context, _ *adksession.ListRequest) (*adksession.ListResponse, error) {
	return nil, errors.New("not implemented")
}

func (m *mockSessionService) Delete(_ context.Context, _ *adksession.DeleteRequest) error {
	return errors.New("not implemented")
}

func (m *mockSessionService) AppendEvent(_ context.Context, _ adksession.Session, event *adksession.Event) error {
	m.callCount++
	if m.failOnCall > 0 && m.callCount >= m.failOnCall {
		return errors.New("mock AppendEvent failure")
	}
	m.appendedEvents = append(m.appendedEvents, event)
	return nil
}

// --- helpers ---

func makePlugin(queue *csync.Map[string, []SessionAgentCall], sessions *csync.Map[string, adksession.Session], svc adksession.Service) *midLoopPlugin {
	return &midLoopPlugin{
		queue:             queue,
		activeADKSessions: sessions,
		sessionService:    svc,
	}
}

func makeReq(contents ...*genai.Content) *adkmodel.LLMRequest {
	return &adkmodel.LLMRequest{Contents: contents}
}

func makeCall(prompt string) SessionAgentCall {
	return SessionAgentCall{SessionID: "sess-1", Prompt: prompt}
}

// --- tests ---

func TestMidLoop_NoQueuedMessages(t *testing.T) {
	t.Parallel()

	queue := csync.NewMap[string, []SessionAgentCall]()
	sessions := csync.NewMap[string, adksession.Session]()
	svc := &mockSessionService{}
	p := makePlugin(queue, sessions, svc)

	req := makeReq()
	resp, err := p.beforeModel(newMockCtx("sess-1"), req)

	require.NoError(t, err)
	assert.Nil(t, resp)
	assert.Empty(t, req.Contents)
	assert.Empty(t, svc.appendedEvents)
}

func TestMidLoop_SingleMessage(t *testing.T) {
	t.Parallel()

	queue := csync.NewMap[string, []SessionAgentCall]()
	sessions := csync.NewMap[string, adksession.Session]()
	svc := &mockSessionService{}
	sessions.Set("sess-1", &mockSession{id: "sess-1"})
	queue.Set("sess-1", []SessionAgentCall{makeCall("hello operator")})
	p := makePlugin(queue, sessions, svc)

	req := makeReq()
	resp, err := p.beforeModel(newMockCtx("sess-1"), req)

	require.NoError(t, err)
	assert.Nil(t, resp)

	// Persisted event has raw text (no XML wrapping).
	require.Len(t, svc.appendedEvents, 1)
	assert.Equal(t, "user", svc.appendedEvents[0].Author)
	assert.Equal(t, "hello operator", svc.appendedEvents[0].Content.Parts[0].Text)

	// Injected content has XML wrapping.
	require.Len(t, req.Contents, 1)
	assert.Equal(t, genai.RoleUser, req.Contents[0].Role)
	assert.Contains(t, req.Contents[0].Parts[0].Text, "<user_message seq=\"1\">")
	assert.Contains(t, req.Contents[0].Parts[0].Text, "hello operator")

	// Queue is drained.
	_, ok := queue.Get("sess-1")
	assert.False(t, ok)
}

func TestMidLoop_MultipleMessages(t *testing.T) {
	t.Parallel()

	queue := csync.NewMap[string, []SessionAgentCall]()
	sessions := csync.NewMap[string, adksession.Session]()
	svc := &mockSessionService{}
	sessions.Set("sess-1", &mockSession{id: "sess-1"})
	queue.Set("sess-1", []SessionAgentCall{
		makeCall("first"),
		makeCall("second"),
		makeCall("third"),
	})
	p := makePlugin(queue, sessions, svc)

	req := makeReq()
	_, err := p.beforeModel(newMockCtx("sess-1"), req)

	require.NoError(t, err)
	require.Len(t, svc.appendedEvents, 3)

	// Sequential seq attributes.
	require.Len(t, req.Contents, 1, "all messages should be appended to one user content")
	parts := req.Contents[0].Parts
	require.Len(t, parts, 3)
	assert.Contains(t, parts[0].Text, `seq="1"`)
	assert.Contains(t, parts[1].Text, `seq="2"`)
	assert.Contains(t, parts[2].Text, `seq="3"`)
}

func TestMidLoop_AppendsToLastUserContent(t *testing.T) {
	t.Parallel()

	queue := csync.NewMap[string, []SessionAgentCall]()
	sessions := csync.NewMap[string, adksession.Session]()
	svc := &mockSessionService{}
	sessions.Set("sess-1", &mockSession{id: "sess-1"})
	queue.Set("sess-1", []SessionAgentCall{makeCall("injected")})
	p := makePlugin(queue, sessions, svc)

	// Existing user content with text-only parts.
	existing := &genai.Content{
		Role:  genai.RoleUser,
		Parts: []*genai.Part{{Text: "original prompt"}},
	}
	req := makeReq(existing)
	_, err := p.beforeModel(newMockCtx("sess-1"), req)

	require.NoError(t, err)
	// Should append to the existing content, not create a new one.
	require.Len(t, req.Contents, 1)
	require.Len(t, req.Contents[0].Parts, 2)
	assert.Equal(t, "original prompt", req.Contents[0].Parts[0].Text)
	assert.Contains(t, req.Contents[0].Parts[1].Text, "injected")
}

func TestMidLoop_NewContentAfterFunctionResponse(t *testing.T) {
	t.Parallel()

	queue := csync.NewMap[string, []SessionAgentCall]()
	sessions := csync.NewMap[string, adksession.Session]()
	svc := &mockSessionService{}
	sessions.Set("sess-1", &mockSession{id: "sess-1"})
	queue.Set("sess-1", []SessionAgentCall{makeCall("injected")})
	p := makePlugin(queue, sessions, svc)

	// Last content is user-role with FunctionResponse.
	frContent := &genai.Content{
		Role: genai.RoleUser,
		Parts: []*genai.Part{{
			FunctionResponse: &genai.FunctionResponse{Name: "draft", ID: "fc-1"},
		}},
	}
	req := makeReq(frContent)
	_, err := p.beforeModel(newMockCtx("sess-1"), req)

	require.NoError(t, err)
	// Should NOT append to the FunctionResponse content.
	require.Len(t, req.Contents, 2)
	assert.NotNil(t, req.Contents[0].Parts[0].FunctionResponse)
	assert.Equal(t, genai.RoleUser, req.Contents[1].Role)
	assert.Contains(t, req.Contents[1].Parts[0].Text, "injected")
}

func TestMidLoop_CreatesNewContentAfterModel(t *testing.T) {
	t.Parallel()

	queue := csync.NewMap[string, []SessionAgentCall]()
	sessions := csync.NewMap[string, adksession.Session]()
	svc := &mockSessionService{}
	sessions.Set("sess-1", &mockSession{id: "sess-1"})
	queue.Set("sess-1", []SessionAgentCall{makeCall("injected")})
	p := makePlugin(queue, sessions, svc)

	// Last content is model-role.
	modelContent := &genai.Content{
		Role:  genai.RoleModel,
		Parts: []*genai.Part{{Text: "model says"}},
	}
	req := makeReq(modelContent)
	_, err := p.beforeModel(newMockCtx("sess-1"), req)

	require.NoError(t, err)
	require.Len(t, req.Contents, 2)
	assert.Equal(t, genai.RoleModel, req.Contents[0].Role)
	assert.Equal(t, genai.RoleUser, req.Contents[1].Role)
}

func TestMidLoop_SkipsOnCanceledContext(t *testing.T) {
	t.Parallel()

	queue := csync.NewMap[string, []SessionAgentCall]()
	sessions := csync.NewMap[string, adksession.Session]()
	svc := &mockSessionService{}
	sessions.Set("sess-1", &mockSession{id: "sess-1"})
	queue.Set("sess-1", []SessionAgentCall{makeCall("should not drain")})
	p := makePlugin(queue, sessions, svc)

	req := makeReq()
	resp, err := p.beforeModel(newCanceledMockCtx("sess-1"), req)

	// Returns nil,nil — cancel handler owns cleanup.
	require.NoError(t, err)
	assert.Nil(t, resp)

	// Queue should NOT be drained.
	msgs, ok := queue.Get("sess-1")
	assert.True(t, ok)
	assert.Len(t, msgs, 1)
}

func TestMidLoop_PersistBeforeInject(t *testing.T) {
	t.Parallel()

	queue := csync.NewMap[string, []SessionAgentCall]()
	sessions := csync.NewMap[string, adksession.Session]()
	svc := &mockSessionService{}
	sessions.Set("sess-1", &mockSession{id: "sess-1"})
	queue.Set("sess-1", []SessionAgentCall{makeCall("persist first")})
	p := makePlugin(queue, sessions, svc)

	req := makeReq()
	_, err := p.beforeModel(newMockCtx("sess-1"), req)

	require.NoError(t, err)
	// AppendEvent was called (persist happened).
	require.Len(t, svc.appendedEvents, 1)
	// And injection happened too.
	require.Len(t, req.Contents, 1)
}

func TestMidLoop_RequeueOnAppendEventFailure(t *testing.T) {
	t.Parallel()

	queue := csync.NewMap[string, []SessionAgentCall]()
	sessions := csync.NewMap[string, adksession.Session]()
	svc := &mockSessionService{failOnCall: 2} // Fail on 2nd call
	sessions.Set("sess-1", &mockSession{id: "sess-1"})
	queue.Set("sess-1", []SessionAgentCall{
		makeCall("msg-1"),
		makeCall("msg-2"),
		makeCall("msg-3"),
	})
	p := makePlugin(queue, sessions, svc)

	req := makeReq()
	_, err := p.beforeModel(newMockCtx("sess-1"), req)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "AppendEvent failed")

	// msg-1 was persisted successfully.
	require.Len(t, svc.appendedEvents, 1)
	assert.Equal(t, "msg-1", svc.appendedEvents[0].Content.Parts[0].Text)

	// msg-2 and msg-3 were re-queued.
	requeued, ok := queue.Get("sess-1")
	require.True(t, ok)
	require.Len(t, requeued, 2)
	assert.Equal(t, "msg-2", requeued[0].Prompt)
	assert.Equal(t, "msg-3", requeued[1].Prompt)
}

func TestMidLoop_NoActiveSession(t *testing.T) {
	t.Parallel()

	queue := csync.NewMap[string, []SessionAgentCall]()
	sessions := csync.NewMap[string, adksession.Session]()
	svc := &mockSessionService{}
	// Do NOT set any session in activeADKSessions.
	queue.Set("sess-1", []SessionAgentCall{makeCall("orphan")})
	p := makePlugin(queue, sessions, svc)

	req := makeReq()
	_, err := p.beforeModel(newMockCtx("sess-1"), req)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "no active ADK session")

	// All messages re-queued.
	requeued, ok := queue.Get("sess-1")
	require.True(t, ok)
	require.Len(t, requeued, 1)
	assert.Equal(t, "orphan", requeued[0].Prompt)
}

func TestMidLoop_DrainIsAtomic(t *testing.T) {
	t.Parallel()

	queue := csync.NewMap[string, []SessionAgentCall]()
	sessions := csync.NewMap[string, adksession.Session]()
	svc := &mockSessionService{}
	sessions.Set("sess-1", &mockSession{id: "sess-1"})
	queue.Set("sess-1", []SessionAgentCall{makeCall("a"), makeCall("b")})
	p := makePlugin(queue, sessions, svc)

	req := makeReq()
	_, err := p.beforeModel(newMockCtx("sess-1"), req)

	require.NoError(t, err)
	// Queue should be empty after injection.
	_, ok := queue.Get("sess-1")
	assert.False(t, ok)
}

func TestMidLoop_WrongSession(t *testing.T) {
	t.Parallel()

	queue := csync.NewMap[string, []SessionAgentCall]()
	sessions := csync.NewMap[string, adksession.Session]()
	svc := &mockSessionService{}
	sessions.Set("sess-A", &mockSession{id: "sess-A"})
	queue.Set("sess-A", []SessionAgentCall{makeCall("for A")})
	p := makePlugin(queue, sessions, svc)

	// Plugin called with session B — nothing to inject.
	req := makeReq()
	resp, err := p.beforeModel(newMockCtx("sess-B"), req)

	require.NoError(t, err)
	assert.Nil(t, resp)
	assert.Empty(t, req.Contents)

	// sess-A's queue untouched.
	msgs, ok := queue.Get("sess-A")
	assert.True(t, ok)
	assert.Len(t, msgs, 1)
}

func TestMidLoop_PersistsRawPartsNotXML(t *testing.T) {
	t.Parallel()

	queue := csync.NewMap[string, []SessionAgentCall]()
	sessions := csync.NewMap[string, adksession.Session]()
	svc := &mockSessionService{}
	sessions.Set("sess-1", &mockSession{id: "sess-1"})
	queue.Set("sess-1", []SessionAgentCall{makeCall("raw text")})
	p := makePlugin(queue, sessions, svc)

	req := makeReq()
	_, err := p.beforeModel(newMockCtx("sess-1"), req)

	require.NoError(t, err)

	// Persisted event has raw text (no XML).
	persistedText := svc.appendedEvents[0].Content.Parts[0].Text
	assert.Equal(t, "raw text", persistedText)
	assert.NotContains(t, persistedText, "<user_message")

	// Injected content has XML.
	injectedText := req.Contents[0].Parts[0].Text
	assert.Contains(t, injectedText, "<user_message")
}

// --- callToGenaiParts tests ---

func TestCallToGenaiParts_TextOnly(t *testing.T) {
	t.Parallel()

	parts := callToGenaiParts(SessionAgentCall{Prompt: "hello"})
	require.Len(t, parts, 1)
	assert.Equal(t, "hello", parts[0].Text)
}

func TestCallToGenaiParts_TextAttachment(t *testing.T) {
	t.Parallel()

	call := SessionAgentCall{
		Prompt: "check this",
		Attachments: []message.Attachment{{
			FilePath: "test.go",
			MimeType: "text/plain",
			Content:  []byte("package main"),
		}},
	}
	parts := callToGenaiParts(call)
	require.Len(t, parts, 1)
	// Text attachment is merged into prompt text.
	assert.Contains(t, parts[0].Text, "check this")
	assert.Contains(t, parts[0].Text, "package main")
}

func TestCallToGenaiParts_BinaryAttachment(t *testing.T) {
	t.Parallel()

	call := SessionAgentCall{
		Prompt: "look at this",
		Attachments: []message.Attachment{{
			FilePath: "photo.png",
			MimeType: "image/png",
			Content:  []byte{0x89, 0x50, 0x4E, 0x47},
		}},
	}
	parts := callToGenaiParts(call)
	require.Len(t, parts, 2)
	assert.Equal(t, "look at this", parts[0].Text)
	assert.NotNil(t, parts[1].InlineData)
	assert.Equal(t, "image/png", parts[1].InlineData.MIMEType)
}

func TestCallToGenaiParts_Empty(t *testing.T) {
	t.Parallel()

	parts := callToGenaiParts(SessionAgentCall{})
	assert.Empty(t, parts)
}

// --- wrapOperatorMessage tests ---

func TestWrapOperatorMessage_TextWrapped(t *testing.T) {
	t.Parallel()

	parts := []*genai.Part{{Text: "hello"}}
	wrapped := wrapOperatorMessage(parts, 1)
	require.Len(t, wrapped, 1)
	assert.Contains(t, wrapped[0].Text, `<user_message seq="1">`)
	assert.Contains(t, wrapped[0].Text, "hello")
	assert.Contains(t, wrapped[0].Text, `</user_message>`)
}

func TestWrapOperatorMessage_InlineDataPassthrough(t *testing.T) {
	t.Parallel()

	parts := []*genai.Part{
		{Text: "text"},
		{InlineData: &genai.Blob{MIMEType: "image/png", Data: []byte{1}}},
	}
	wrapped := wrapOperatorMessage(parts, 2)
	require.Len(t, wrapped, 2)
	assert.Contains(t, wrapped[0].Text, `seq="2"`)
	assert.NotNil(t, wrapped[1].InlineData, "InlineData should pass through unwrapped")
}

// --- injectUserParts tests ---

func TestInjectUserParts_EmptyContents(t *testing.T) {
	t.Parallel()

	req := makeReq()
	injectUserParts(req, []*genai.Part{{Text: "injected"}})
	require.Len(t, req.Contents, 1)
	assert.Equal(t, genai.RoleUser, req.Contents[0].Role)
}

func TestInjectUserParts_AppendsToTextOnlyUser(t *testing.T) {
	t.Parallel()

	existing := &genai.Content{
		Role:  genai.RoleUser,
		Parts: []*genai.Part{{Text: "original"}},
	}
	req := makeReq(existing)
	injectUserParts(req, []*genai.Part{{Text: "injected"}})
	require.Len(t, req.Contents, 1)
	require.Len(t, req.Contents[0].Parts, 2)
}

func TestInjectUserParts_NewAfterFunctionCall(t *testing.T) {
	t.Parallel()

	existing := &genai.Content{
		Role: genai.RoleUser,
		Parts: []*genai.Part{{
			FunctionCall: &genai.FunctionCall{Name: "test"},
		}},
	}
	req := makeReq(existing)
	injectUserParts(req, []*genai.Part{{Text: "injected"}})
	require.Len(t, req.Contents, 2)
}
