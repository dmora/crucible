package dialog

import (
	"testing"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"github.com/dmora/crucible/internal/askuser"
	"github.com/dmora/crucible/internal/ui/common"
	"github.com/dmora/crucible/internal/ui/styles"
	"github.com/stretchr/testify/require"
)

func testCommon() *common.Common {
	s := styles.NewStyles(styles.DefaultTheme, false)
	return &common.Common{Styles: &s}
}

func TestNewQuestionState_MultiSelectOverridesConfirm(t *testing.T) {
	t.Parallel()
	com := testCommon()
	q := askuser.Question{
		ID:          "test",
		Question:    "Pick options",
		MultiSelect: true,
		Options: []askuser.Option{
			{Label: "Yes"},
			{Label: "No"},
		},
	}

	qs := newQuestionState(com, q)
	require.Equal(t, questionTypeMultiSelect, qs.qType)
}

func TestNewQuestionState_ConfirmWithoutMultiSelect(t *testing.T) {
	t.Parallel()
	com := testCommon()
	q := askuser.Question{
		ID:       "test",
		Question: "Continue?",
		Options: []askuser.Option{
			{Label: "Yes"},
			{Label: "No"},
		},
	}

	qs := newQuestionState(com, q)
	require.Equal(t, questionTypeConfirm, qs.qType)
}

func TestNewQuestionState_FreeText(t *testing.T) {
	t.Parallel()
	com := testCommon()
	q := askuser.Question{
		ID:       "test",
		Question: "What do you think?",
	}

	qs := newQuestionState(com, q)
	require.Equal(t, questionTypeFreeText, qs.qType)
}

func TestNewQuestionState_SingleSelect(t *testing.T) {
	t.Parallel()
	com := testCommon()
	q := askuser.Question{
		ID:       "test",
		Question: "Pick one",
		Options: []askuser.Option{
			{Label: "A"},
			{Label: "B"},
			{Label: "C"},
		},
	}

	qs := newQuestionState(com, q)
	require.Equal(t, questionTypeSingleSelect, qs.qType)
}

func TestToggleKeyMatchesSpace(t *testing.T) {
	t.Parallel()
	km := defaultAskUserKeyMap()
	spaceMsg := tea.KeyPressMsg{Code: tea.KeySpace}
	require.True(t, key.Matches(spaceMsg, km.Toggle), "Toggle binding should match space key press")
}
