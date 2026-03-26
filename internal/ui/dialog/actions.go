package dialog

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"

	tea "charm.land/bubbletea/v2"
	"github.com/dmora/crucible/internal/askuser"
	"github.com/dmora/crucible/internal/commands"
	"github.com/dmora/crucible/internal/config"
	"github.com/dmora/crucible/internal/message"
	"github.com/dmora/crucible/internal/oauth"
	"github.com/dmora/crucible/internal/permission"
	"github.com/dmora/crucible/internal/session"
	"github.com/dmora/crucible/internal/ui/common"
	"github.com/dmora/crucible/internal/ui/util"
)

// AuthID is the identifier for the Auth dialog.
const AuthID = "auth"

// ActionCredentialReady is returned by auth sub-dialogs when the user has
// provided a credential. ui.go calls SetProviderCredential + UpdateAgentModel.
type ActionCredentialReady struct {
	ProviderID string
	Credential config.Credential
	// SelectModel, when non-nil, causes ui.go to re-emit ActionSelectModel
	// after persisting the credential. This resumes the model-selection flow
	// that was interrupted by first-time auth or re-authentication.
	SelectModel *ActionSelectModel
}

// ActionOpenSubDialog is returned when a dialog wants to push another dialog
// onto the overlay stack. Carries the constructed dialog and its init command.
type ActionOpenSubDialog struct {
	Dialog Dialog
	Cmd    tea.Cmd
}

// ActionClose is a message to close the current dialog.
type ActionClose struct{}

// ActionQuit is a message to quit the application.
type ActionQuit = tea.QuitMsg

// ActionOpenDialog is a message to open a dialog.
type ActionOpenDialog struct {
	DialogID string
}

// ActionSelectSession is a message indicating a session has been selected.
type ActionSelectSession struct {
	Session session.Session
}

// ActionSelectModel is a message indicating a model has been selected.
type ActionSelectModel struct {
	Provider       config.ProviderMetadata
	Model          config.SelectedModel
	ModelType      config.SelectedModelType
	ReAuthenticate bool
}

// Messages for commands
type (
	ActionNewSession        struct{}
	ActionToggleHelp        struct{}
	ActionToggleCompactMode struct{}
	ActionToggleThinking    struct{}
	ActionTogglePills       struct{}
	ActionExternalEditor    struct{}
	ActionToggleYoloMode    struct{}
	ActionToggleHold        struct{}
	// ActionInitializeProject is a message to initialize a project.
	ActionInitializeProject struct{}
	ActionSummarize         struct {
		SessionID string
	}
	// ActionSelectReasoningEffort is a message indicating a reasoning effort has been selected.
	ActionSelectReasoningEffort struct {
		Effort string
	}
	// ActionSelectTheme is a message indicating a theme has been selected.
	ActionSelectTheme struct {
		ThemeID string
	}
	// ActionSelectSpinner is a message indicating a spinner preset has been selected.
	ActionSelectSpinner struct {
		Preset string
	}
	// ActionSetTransparent is a message indicating the transparent mode has been toggled.
	ActionSetTransparent struct {
		Transparent bool
	}
	ActionPermissionResponse struct {
		Permission permission.PermissionRequest
		Action     PermissionAction
	}
	// ActionAskUserResponse is a message indicating the operator responded to an ask_user dialog.
	ActionAskUserResponse struct {
		RequestID  string
		ToolCallID string
		Response   askuser.Response
	}
	// ActionRunCustomCommand is a message to run a custom command.
	ActionRunCustomCommand struct {
		Content   string
		Arguments []commands.Argument
		Args      map[string]string // Actual argument values
	}
	// ActionSkipPlan sets a session-level flag that bypasses artifact enforcement
	// for all remaining dispatches in this session.
	ActionSkipPlan struct{}
	// ActionReloadStations signals that station configuration changed and
	// processManagers should be reconciled.
	ActionReloadStations struct {
		Stations []string // names of changed stations (empty = full reload)
	}
	// ActionShowFactoryStatus is a message to show the factory status banner.
	ActionShowFactoryStatus struct{}
	// ActionShowEquipment is a message to open the equipment inspector dialog.
	ActionShowEquipment struct{}
	// ActionSelectRelay is a message indicating a relay station has been selected.
	ActionSelectRelay struct {
		Station string // empty = exit relay (back to supervisor)
	}
	// ActionRunMCPPrompt is a message to run a custom command.
	ActionRunMCPPrompt struct {
		Title       string
		Description string
		PromptID    string
		ClientID    string
		Arguments   []commands.Argument
		Args        map[string]string // Actual argument values
	}
)

// Messages for API key input dialog.
type (
	ActionChangeAPIKeyState struct {
		State APIKeyInputState
	}
)

// Messages for OAuth2 authorization code flow dialog.
type (
	// ActionInitiateOAuth is sent when the auth flow is initiated and
	// the browser should be opened.
	ActionInitiateOAuth struct {
		AuthURL string
		Port    int
	}

	// ActionCompleteOAuth is sent when the OAuth flow completes successfully.
	ActionCompleteOAuth struct {
		Token *oauth.Token
	}

	// ActionOAuthErrored is sent when the OAuth flow encounters an error.
	ActionOAuthErrored struct {
		Error error
	}
)

// ActionCmd represents an action that carries a [tea.Cmd] to be passed to the
// Bubble Tea program loop.
type ActionCmd struct {
	Cmd tea.Cmd
}

// ActionFilePickerSelected is a message indicating a file has been selected in
// the file picker dialog.
type ActionFilePickerSelected struct {
	Path string
}

// Cmd returns a command that reads the file at path and sends a
// [message.Attachement] to the program.
func (a ActionFilePickerSelected) Cmd() tea.Cmd {
	path := a.Path
	if path == "" {
		return nil
	}
	return func() tea.Msg {
		isFileLarge, err := common.IsFileTooBig(path, common.MaxAttachmentSize)
		if err != nil {
			return util.InfoMsg{
				Type: util.InfoTypeError,
				Msg:  fmt.Sprintf("unable to read the image: %v", err),
			}
		}
		if isFileLarge {
			return util.InfoMsg{
				Type: util.InfoTypeError,
				Msg:  "file too large, max 5MB",
			}
		}

		content, err := os.ReadFile(path)
		if err != nil {
			return util.InfoMsg{
				Type: util.InfoTypeError,
				Msg:  fmt.Sprintf("unable to read the image: %v", err),
			}
		}

		mimeBufferSize := min(512, len(content))
		mimeType := http.DetectContentType(content[:mimeBufferSize])
		fileName := filepath.Base(path)

		return message.Attachment{
			FilePath: path,
			FileName: fileName,
			MimeType: mimeType,
			Content:  content,
		}
	}
}
