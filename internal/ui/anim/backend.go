package anim

import tea "charm.land/bubbletea/v2"

// SpinnerBackend is the interface satisfied by both the industrial gradient
// scramble (*Anim) and classic frame-cycling spinners (*ClassicSpinner).
type SpinnerBackend interface {
	Start() tea.Cmd
	Animate(msg StepMsg) tea.Cmd
	StepOnce() // advance one frame without scheduling a next tick
	Render() string
	SetLabel(label string)
	Width() int
	FollowsText() bool // whether the spinner should chase streamed text
}

// Compile-time check: *Anim satisfies SpinnerBackend.
var _ SpinnerBackend = (*Anim)(nil)
