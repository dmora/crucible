#!/usr/bin/env bash
set -euo pipefail

CRUCIBLE_BIN="./bin/crucible"
SESSION="crucible-spinner-test"
CONFIG_DIR=$(mktemp -d)
DATA_DIR=$(mktemp -d)
export XDG_CONFIG_HOME="$CONFIG_DIR"
export XDG_DATA_HOME="$DATA_DIR"

# Use a local socket directory for tmux (avoids /private/tmp permission issues)
TMUX_TMPDIR="${TMUX_TMPDIR:-$(mktemp -d "${TMPDIR:-/tmp}/tmux-test-XXXXXX")}"
export TMUX_TMPDIR

cleanup() {
    tmux kill-session -t "$SESSION" 2>/dev/null || true
    rm -rf "$CONFIG_DIR" "$DATA_DIR" "$TMUX_TMPDIR"
}
trap cleanup EXIT

# Build first
go build -o "$CRUCIBLE_BIN" .

# Helper: send keys and capture pane content
send_keys() { tmux send-keys -t "$SESSION" "$@"; }
capture() { tmux capture-pane -t "$SESSION" -p; }
wait_for() {
    local pattern="$1" timeout="${2:-10}"
    for i in $(seq 1 "$timeout"); do
        if capture | grep -qP "$pattern" 2>/dev/null || capture | grep -q "$pattern" 2>/dev/null; then return 0; fi
        sleep 1
    done
    echo "FAIL: timed out waiting for pattern: $pattern" >&2
    echo "--- Pane content ---" >&2
    capture >&2
    echo "---" >&2
    return 1
}

# open_spinner_picker: opens command palette, filters to "Switch Spinner", selects it
open_spinner_picker() {
    send_keys "C-p"
    sleep 1
    send_keys "spinner"
    sleep 0.5
    send_keys Enter
    sleep 1
}

config_file="$DATA_DIR/crucible/crucible.json"

# === AC#11: Launch ===
echo "=== AC#11: Launch ==="
tmux new-session -d -s "$SESSION" -x 120 -y 40 "$CRUCIBLE_BIN -d"
wait_for "CRUCIBLE\|>" 15
echo "PASS: Crucible launched"

# === AC#12: Picker renders ===
echo "=== AC#12: Picker renders ==="
open_spinner_picker
output=$(capture)
# (verbose output suppressed to keep logs concise)
for preset in Industrial Pulse Dots Ellipsis Points Meter; do
    if ! echo "$output" | grep -q "$preset"; then
        echo "FAIL: preset '$preset' not found in picker"
        exit 1
    fi
done
echo "PASS: All 6 presets listed"
send_keys Escape
sleep 0.5

# === AC#13: Select pulse ===
echo "=== AC#13: Select pulse ==="
open_spinner_picker
send_keys "pulse"
sleep 0.5
send_keys Enter
sleep 2
if [ -f "$config_file" ]; then
    config_val=$(python3 -c "import sys,json; print(json.load(open('$config_file')).get('options',{}).get('tui',{}).get('spinner',''))")
    if [ "$config_val" = "pulse" ]; then
        echo "PASS: Config correctly set to pulse"
    else
        echo "FAIL: Expected config spinner=pulse, got '$config_val'"
        exit 1
    fi
else
    echo "FAIL: Config file not found at $config_file"
    exit 1
fi

# === AC#14: Select dots ===
echo "=== AC#14: Select dots ==="
open_spinner_picker
send_keys "dots"
sleep 0.5
send_keys Enter
sleep 1
config_val=$(python3 -c "import json; print(json.load(open('$config_file')).get('options',{}).get('tui',{}).get('spinner',''))")
if [ "$config_val" = "dots" ]; then
    echo "PASS: Config correctly set to dots"
else
    echo "FAIL: Expected config spinner=dots, got '$config_val'"
    exit 1
fi

# === AC#15: Persistence ===
echo "=== AC#15: Persistence ==="
tmux kill-session -t "$SESSION"
sleep 1
tmux new-session -d -s "$SESSION" -x 120 -y 40 "$CRUCIBLE_BIN -d"
wait_for "CRUCIBLE\|>" 15
# Verify config still has dots
config_val=$(python3 -c "import json; print(json.load(open('$config_file')).get('options',{}).get('tui',{}).get('spinner',''))")
if [ "$config_val" = "dots" ]; then
    echo "PASS: dots preset persisted across restart"
else
    echo "FAIL: Expected config spinner=dots after restart, got '$config_val'"
    exit 1
fi

# === AC#16: Restore default ===
echo "=== AC#16: Restore default ==="
open_spinner_picker
send_keys "industrial"
sleep 0.5
send_keys Enter
sleep 1
config_val=$(python3 -c "import json; print(json.load(open('$config_file')).get('options',{}).get('tui',{}).get('spinner',''))")
if [ "$config_val" = "industrial" ]; then
    echo "PASS: Restored to industrial"
else
    echo "FAIL: Expected config spinner=industrial, got '$config_val'"
    exit 1
fi

echo ""
echo "=== ALL INTEGRATION TESTS PASSED ==="
