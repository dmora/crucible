package model

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/dmora/crucible/internal/agent"
	"github.com/dmora/crucible/internal/config"
	"github.com/dmora/crucible/internal/ui/chat"
	"github.com/dmora/crucible/internal/ui/common"
	"github.com/dmora/crucible/internal/ui/styles"
)

// sessionProcessStates returns process states filtered to the current session.
func (m *UI) sessionProcessStates() map[string]agent.ProcessInfo {
	if m.session == nil {
		return nil
	}
	filtered := make(map[string]agent.ProcessInfo)
	for k, info := range m.processStates {
		if info.SessionID == m.session.ID {
			filtered[k] = info
		}
	}
	return filtered
}

// processInfo renders the Stations section showing dispatch history + waiting stations.
func (m *UI) processInfo(width, maxLines int, isSection bool) string {
	t := m.com.Styles

	title := t.ResourceGroupTitle.Render("Stations")
	if isSection {
		title = common.Section(t, title, width)
	}

	states := m.sessionProcessStates()
	stations := m.com.Config().Stations
	list := stationTimeline(t, m.dispatchLog, states, stations, width, maxLines)

	return lipgloss.NewStyle().Width(width).Render(fmt.Sprintf("%s\n\n%s", title, list))
}

// stationTimeline renders a chronological dispatch history with waiting stations at the bottom.
func stationTimeline(
	t *styles.Styles,
	log []agent.DispatchEntry,
	states map[string]agent.ProcessInfo,
	stations map[string]config.StationConfig,
	width, maxLines int,
) string {
	if maxLines <= 0 {
		return ""
	}

	// Split dispatch log into completed vs running.
	var completed []string
	var runningEntry string
	for _, entry := range log {
		if entry.Verdict == agent.VerdictRunning {
			runningEntry = renderRunningDispatch(t, entry, states, width)
		} else {
			completed = append(completed, renderCompletedDispatch(t, entry, states, width))
		}
	}

	waiting := waitingStationRows(t, log, stations, width)

	// Budget lines: running = 2, waiting = 1 each, completed = 2 each.
	reserved := len(waiting) // 1 line per waiting
	if runningEntry != "" {
		reserved += 2
	}
	availableLines := maxLines - reserved
	maxCompleted := max(0, availableLines/2) // each completed entry = 2 lines
	completed = truncateCompleted(t, completed, maxCompleted)

	// Assemble: completed + running + waiting.
	var rendered []string
	rendered = append(rendered, completed...)
	if runningEntry != "" {
		rendered = append(rendered, runningEntry)
	}
	rendered = append(rendered, waiting...)

	if len(rendered) == 0 {
		return t.ResourceAdditionalText.Render("None")
	}
	return lipgloss.JoinVertical(lipgloss.Left, rendered...)
}

// waitingStationRows returns rendered rows for stations not yet dispatched.
func waitingStationRows(
	t *styles.Styles,
	log []agent.DispatchEntry,
	stations map[string]config.StationConfig,
	width int,
) []string {
	dispatched := make(map[string]bool, len(log))
	for _, entry := range log {
		dispatched[entry.Station] = true
	}
	names := make([]string, 0, len(stations))
	for name := range stations {
		if !dispatched[name] {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	rows := make([]string, len(names))
	for i, name := range names {
		rows[i] = renderWaitingStation(t, name, width)
	}
	return rows
}

// truncateCompleted drops oldest completed entries to fit within maxCompleted.
func truncateCompleted(t *styles.Styles, completed []string, maxCompleted int) []string {
	if len(completed) <= maxCompleted {
		return completed
	}
	dropped := len(completed) - maxCompleted
	completed = completed[dropped:]
	if maxCompleted > 0 {
		completed[0] = t.ResourceAdditionalText.Render(fmt.Sprintf("…%d earlier", dropped))
	}
	return completed
}

// renderCompletedDispatch renders a single completed dispatch entry with detail line.
func renderCompletedDispatch(t *styles.Styles, entry agent.DispatchEntry, states map[string]agent.ProcessInfo, width int) string {
	var icon, chip string
	switch entry.Verdict {
	case agent.VerdictDone:
		icon = t.ResourceOnlineIcon.String()
		chip = "done"
	case agent.VerdictFailed:
		icon = t.ResourceErrorIcon.String()
		chip = "failed"
	case agent.VerdictCanceled:
		icon = t.ResourceOfflineIcon.String()
		chip = "canceled"
	default:
		icon = t.ResourceOfflineIcon.String()
		chip = "unknown"
	}

	dur := formatElapsedDuration(entry.Duration)
	desc := fmt.Sprintf("%s  %s", chip, dur)
	description := t.ResourceStatus.Render(desc)

	statusLine := common.Status(t, common.StatusOpts{
		Icon:        icon,
		Title:       t.ResourceName.Render(entry.Station),
		Description: description,
	}, width)

	// Detail line: model + fuel gauge.
	detail := completedDetail(t, entry, states)
	if detail != "" {
		indent := lipgloss.NewStyle().PaddingLeft(lipgloss.Width(icon) + 1)
		return lipgloss.JoinVertical(lipgloss.Left, statusLine, indent.Render(detail))
	}
	return statusLine
}

// completedDetail renders model + fuel gauge for a completed dispatch entry.
func completedDetail(t *styles.Styles, entry agent.DispatchEntry, states map[string]agent.ProcessInfo) string {
	var model string
	for _, pi := range states {
		if pi.Station == entry.Station && pi.Model != "" {
			model = pi.Model
			break
		}
	}
	var parts []string
	if model != "" {
		parts = append(parts, t.Subtle.Render(model))
	}
	if entry.ContextUsed > 0 {
		parts = append(parts, t.Subtle.Render(fuelGauge(entry.ContextUsed, entry.ContextSize)))
	}
	return strings.Join(parts, " ")
}

// renderRunningDispatch renders the currently running dispatch entry with detail line.
func renderRunningDispatch(
	t *styles.Styles,
	entry agent.DispatchEntry,
	states map[string]agent.ProcessInfo,
	width int,
) string {
	icon := t.ResourceOnlineIcon.String()
	title := t.ResourceName.Render(entry.Station)

	// Find live process info for operator state + detail.
	var description string
	var info agent.ProcessInfo
	var found bool
	for _, pi := range states {
		if pi.Station == entry.Station {
			info = pi
			found = true
			break
		}
	}

	if found {
		opState := chat.DeriveOperatorState(
			info.Activity, info.Phase,
			chat.ToolStatusRunning,
			false,
		)
		if opState != agent.OpStateIdle {
			description = t.ResourceStatus.Render(string(opState))
		}
	}

	// Add elapsed time.
	if elapsed := common.FormatElapsed(entry.StartedAt); elapsed != "" {
		if description != "" {
			description += " " + t.Subtle.Render(elapsed)
		} else {
			description = t.Subtle.Render(elapsed)
		}
	}

	statusLine := common.Status(t, common.StatusOpts{
		Icon:        icon,
		Title:       title,
		Description: description,
	}, width)

	// Detail line: model + fuel gauge.
	if found {
		detail := processDetail(t, info)
		if detail != "" {
			indent := lipgloss.NewStyle().PaddingLeft(lipgloss.Width(icon) + 1)
			return lipgloss.JoinVertical(lipgloss.Left, statusLine, indent.Render(detail))
		}
	}
	return statusLine
}

// renderWaitingStation renders a station that hasn't been dispatched yet.
func renderWaitingStation(t *styles.Styles, name string, width int) string {
	icon := t.ResourceOfflineIcon.String()
	return common.Status(t, common.StatusOpts{
		Icon:        icon,
		Title:       t.ResourceName.Render(name),
		Description: t.ResourceAdditionalText.Render("waiting"),
	}, width)
}

// processDetail renders the model name and context fuel gauge.
// Shows detail for both running and stopped processes (hydrated state).
func processDetail(t *styles.Styles, info agent.ProcessInfo) string {
	if info.State != agent.ProcessStateRunning && info.State != agent.ProcessStateStopped {
		return ""
	}
	var parts []string
	if info.Model != "" {
		parts = append(parts, t.Subtle.Render(info.Model))
	}
	if info.ContextUsed > 0 {
		parts = append(parts, t.Subtle.Render(fuelGauge(info.ContextUsed, info.ContextSize)))
	}
	return strings.Join(parts, " ")
}

// formatElapsedDuration renders a duration as "M:SS" (e.g. "2:14", "0:45").
func formatElapsedDuration(d time.Duration) string {
	mins := int(d.Minutes())
	secs := int(d.Seconds()) % 60
	return fmt.Sprintf("%d:%02d", mins, secs)
}

// updateStationActivity finds active station tool items in the chat
// and updates them with the latest activity from ProcessInfo.
func (m *UI) updateStationActivity() {
	if m.session == nil {
		return
	}

	// Collect all running station infos for this session.
	stationInfos := make(map[string]agent.ProcessInfo)
	for _, info := range m.processStates {
		if info.SessionID == m.session.ID && info.Station != "" {
			stationInfos[info.Station] = info
		}
	}
	if len(stationInfos) == 0 {
		return
	}

	// Resolve worktree branch for this session (empty if worktrees disabled).
	var wtBranch string
	if m.com.App.AgentCoordinator != nil {
		if wt := m.com.App.AgentCoordinator.WorktreeInfo(m.session.ID); wt != nil {
			wtBranch = wt.Branch
		}
	}

	// Walk chat items from the end to find running station tools.
	for i := m.chat.Len() - 1; i >= 0; i-- {
		syncStationItem(m.chat.ItemAt(i), stationInfos, wtBranch)
	}
}

// syncStationItem updates a single chat item if it's an active station tool.
func syncStationItem(item chat.MessageItem, infos map[string]agent.ProcessInfo, wtBranch string) {
	if item == nil {
		return
	}
	st, ok := item.(*chat.StationToolMessageItem)
	if !ok {
		return
	}
	if wtBranch != "" {
		st.SetBranch(wtBranch)
	}
	if st.HasResult() || st.Status() == chat.ToolStatusSuccess || st.Status() == chat.ToolStatusError || st.Status() == chat.ToolStatusCanceled {
		return
	}
	if info, ok := infos[st.ToolCall().Name]; ok {
		st.SetActivity(info.Activity, info.Phase, info.StartedAt)
	}
}

// walkActiveStations iterates active station cards from tail to head,
// calling fn for each station that has not completed, failed, or been canceled.
// Walks from the tail — active stations are always near the end.
func (m *UI) walkActiveStations(fn func(*chat.StationToolMessageItem)) {
	for i := m.chat.Len() - 1; i >= 0; i-- {
		st, ok := m.chat.ItemAt(i).(*chat.StationToolMessageItem)
		if !ok {
			continue
		}
		if st.HasResult() || st.Status() == chat.ToolStatusSuccess ||
			st.Status() == chat.ToolStatusError || st.Status() == chat.ToolStatusCanceled {
			continue
		}
		fn(st)
	}
}

// stepActiveStationSpinners advances the spinner one frame on all active
// station cards. Called on agent events so the spinner moves with work rhythm.
func (m *UI) stepActiveStationSpinners() {
	m.walkActiveStations(func(st *chat.StationToolMessageItem) {
		st.StepOnce()
	})
}

// invalidateActiveStationCaches clears the render cache on all active station
// cards so elapsed time and status chip refresh on the next draw.
// Called on a 1-second timer tick — does NOT advance spinners.
func (m *UI) invalidateActiveStationCaches() {
	m.walkActiveStations(func(st *chat.StationToolMessageItem) {
		st.InvalidateCache()
	})
}

// stationEntryCount returns the total line count for station entries in the
// sidebar: 2 lines per dispatched entry (status + detail), 1 per waiting.
func stationEntryCount(log []agent.DispatchEntry, stations map[string]config.StationConfig) int {
	lines := len(log) * 2 // each dispatched entry is 2 lines
	dispatched := make(map[string]bool, len(log))
	for _, entry := range log {
		dispatched[entry.Station] = true
	}
	for name := range stations {
		if !dispatched[name] {
			lines++ // waiting entries are 1 line
		}
	}
	return lines
}

// fuelGauge renders a compact context usage indicator.
// With known capacity: "42k/200k" (tokens). Without: "42k ctx".
func fuelGauge(used, capacity int) string {
	if capacity > 0 {
		return fmt.Sprintf("%s/%s", formatTokens(used), formatTokens(capacity))
	}
	return fmt.Sprintf("%s ctx", formatTokens(used))
}

// formatTokens renders a token count as a compact string (e.g. 1234 → "1.2k", 150000 → "150k").
func formatTokens(n int) string {
	if n < 1000 {
		return fmt.Sprintf("%d", n)
	}
	k := float64(n) / 1000
	if k < 10 {
		return fmt.Sprintf("%.1fk", k)
	}
	return fmt.Sprintf("%.0fk", k)
}
