package agent

import (
	"os"
	"path/filepath"
	"testing"

	adksession "google.golang.org/adk/session"

	"github.com/dmora/crucible/internal/config"
	"github.com/dmora/crucible/internal/db"
	"github.com/dmora/crucible/internal/message"
	"github.com/dmora/crucible/internal/pubsub"
	"github.com/dmora/crucible/internal/session"
	"github.com/stretchr/testify/require"
)

// fakeEnv is an environment for testing.
type fakeEnv struct {
	workingDir        string
	sessions          session.Service
	messageBroker     *pubsub.Broker[message.Message]
	adkSessionService adksession.Service
}

func testEnv(t *testing.T) fakeEnv {
	workingDir := filepath.Join("/tmp/crucible-test/", t.Name())
	os.RemoveAll(workingDir)

	err := os.MkdirAll(workingDir, 0o755)
	require.NoError(t, err)

	conn, err := db.Connect(t.Context(), t.TempDir())
	require.NoError(t, err)

	q := db.New(conn)
	sessions := session.NewService(q, conn)

	t.Cleanup(func() {
		conn.Close()
		os.RemoveAll(workingDir)
	})

	return fakeEnv{
		workingDir:        workingDir,
		sessions:          sessions,
		messageBroker:     pubsub.NewBroker[message.Message](),
		adkSessionService: adksession.InMemoryService(),
	}
}

func testSessionAgent(env fakeEnv, systemPrompt string) SessionAgent {
	largeModel := Model{
		Metadata: config.ModelMetadata{
			ContextWindow:    200000,
			DefaultMaxTokens: 10000,
		},
	}
	smallModel := Model{
		Metadata: config.ModelMetadata{
			ContextWindow:    200000,
			DefaultMaxTokens: 10000,
		},
	}
	return NewSessionAgent(SessionAgentOptions{
		LargeModel:        largeModel,
		SmallModel:        smallModel,
		SystemPrompt:      systemPrompt,
		Sessions:          env.sessions,
		MessageBroker:     env.messageBroker,
		ADKSessionService: env.adkSessionService,
	})
}

// createSimpleGoProject creates a simple Go project structure in the given directory.
func createSimpleGoProject(t *testing.T, dir string) {
	goMod := `module example.com/testproject

go 1.23
`
	err := os.WriteFile(dir+"/go.mod", []byte(goMod), 0o600)
	require.NoError(t, err)

	mainGo := `package main

import "fmt"

func main() {
	fmt.Println("Hello, World!")
}
`
	err = os.WriteFile(dir+"/main.go", []byte(mainGo), 0o600)
	require.NoError(t, err)
}
