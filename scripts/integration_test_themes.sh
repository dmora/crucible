#!/usr/bin/env bash
# Integration tests for Crucible theme system — acceptance criteria #13-#19
# Runs Crucible in tmux and validates rendering + config persistence.
set -euo pipefail

CRUCIBLE_BIN="/Users/davidmora/Projects/github.com/dmora/crucible/bin/crucible"
SESSION="crucible_integ"
PASS=0
FAIL=0
TOTAL=0

# ---------- helpers ----------

setup_env() {
  # Create isolated temp dir for each test
  TEST_DIR=$(mktemp -d)
  DATA_DIR="$TEST_DIR/data"
  CRUCIBLE_DIR="$TEST_DIR/.crucible"
  mkdir -p "$DATA_DIR" "$CRUCIBLE_DIR"
  # Create init flag to skip initialization dialog
  touch "$CRUCIBLE_DIR/init"
}

teardown_env() {
  tmux kill-session -t "$SESSION" 2>/dev/null || true
  rm -rf "$TEST_DIR"
}

write_config() {
  echo "$1" > "$DATA_DIR/crucible.json"
}

launch_crucible() {
  tmux kill-session -t "$SESSION" 2>/dev/null || true
  tmux new-session -d -s "$SESSION" -x 120 -y 40 \
    "CRUCIBLE_GLOBAL_DATA='$DATA_DIR' GEMINI_API_KEY= GOOGLE_API_KEY= CLAUDECODE= '$CRUCIBLE_BIN' -c '$TEST_DIR' -D '$CRUCIBLE_DIR' 2>'$TEST_DIR/stderr.log'"
  # Wait for app to render
  sleep 3
}

capture_pane() {
  tmux capture-pane -t "$SESSION" -p 2>/dev/null
}

capture_pane_escapes() {
  tmux capture-pane -t "$SESSION" -pe 2>/dev/null
}

read_config() {
  cat "$DATA_DIR/crucible.json" 2>/dev/null || echo "{}"
}

assert_ok() {
  local name="$1"
  local condition="$2"
  TOTAL=$((TOTAL + 1))
  if eval "$condition"; then
    echo "  PASS: $name"
    PASS=$((PASS + 1))
  else
    echo "  FAIL: $name"
    FAIL=$((FAIL + 1))
  fi
}

# ---------- Test 13: Clean-room theme ----------

test_13_clean_room() {
  echo ""
  echo "=== Test 13: Clean-room theme ==="
  setup_env
  write_config '{"options":{"tui":{"theme":"clean-room"}}}'
  launch_crucible

  local capture
  capture=$(capture_pane)

  # Assert CRUCIBLE text appears (block chars: F O U N D R Y)
  assert_ok "CRUCIBLE text visible" \
    'echo "$capture" | grep -q "▀"'

  # Assert no errors in stderr (config read errors)
  local stderr
  stderr=$(cat "$TEST_DIR/stderr.log" 2>/dev/null || echo "")
  assert_ok "No config-read errors in log" \
    '! echo "$stderr" | grep -qi "error.*config\|config.*error\|failed to.*config"'

  teardown_env
}

# ---------- Test 14: Steel-blue baseline ----------

test_14_steel_blue() {
  echo ""
  echo "=== Test 14: Steel-blue (dark theme baseline) ==="
  setup_env
  write_config '{"options":{"tui":{"theme":"steel-blue"}}}'
  launch_crucible

  local capture
  capture=$(capture_pane)

  assert_ok "CRUCIBLE text visible" \
    'echo "$capture" | grep -q "▀"'

  teardown_env
}

# ---------- Test 15: Transparent + dark theme ----------

test_15_transparent_dark() {
  echo ""
  echo "=== Test 15: Transparent + steel-blue ==="
  setup_env
  write_config '{"options":{"tui":{"theme":"steel-blue","transparent":true}}}'
  launch_crucible

  local capture_plain
  capture_plain=$(capture_pane)

  # Assert text content present
  assert_ok "CRUCIBLE text visible" \
    'echo "$capture_plain" | grep -q "▀"'

  # Capture with escape sequences
  local capture_esc
  capture_esc=$(capture_pane_escapes)

  # steel-blue bgBase=#181B20 → RGB(24,27,32) → \x1b[48;2;24;27;32m
  # In transparent mode, this background should NOT appear
  assert_ok "No steel-blue bgBase background escape (48;2;24;27;32)" \
    '! echo "$capture_esc" | grep -q "48;2;24;27;32"'

  teardown_env
}

# ---------- Test 16: Theme switch persistence ----------

test_16_theme_switch_persistence() {
  echo ""
  echo "=== Test 16: Theme switch persistence ==="
  setup_env
  write_config '{"options":{"tui":{"theme":"steel-blue"}}}'
  launch_crucible

  # Verify app started
  local capture
  capture=$(capture_pane)
  assert_ok "App started with steel-blue" \
    'echo "$capture" | grep -q "▀"'

  # Open theme dialog (ctrl+y)
  tmux send-keys -t "$SESSION" C-y
  sleep 1

  # Navigate down to find clean-room (themes listed in order)
  # Theme order: steel-blue, amber-forge, phosphor-green, reactor-red, titanium, clean-room
  # steel-blue is first and pre-selected, so clean-room is 5 down
  tmux send-keys -t "$SESSION" Down Down Down Down Down
  sleep 0.5

  # Select it (Enter)
  tmux send-keys -t "$SESSION" Enter
  sleep 1

  # Read config to verify persistence
  local config
  config=$(read_config)
  assert_ok "Config contains theme:clean-room" \
    'echo "$config" | grep -q "\"clean-room\""'

  teardown_env
}

# ---------- Test 17: Transparent toggle persistence ----------

test_17_transparent_toggle_persistence() {
  echo ""
  echo "=== Test 17: Transparent toggle persistence ==="
  setup_env
  write_config '{"options":{"tui":{"theme":"steel-blue"}}}'
  launch_crucible

  # Verify app started
  local capture
  capture=$(capture_pane)
  assert_ok "App started" \
    'echo "$capture" | grep -q "▀"'

  # Open theme dialog (ctrl+y)
  tmux send-keys -t "$SESSION" C-y
  sleep 1

  # Toggle transparent ('t' key in theme dialog)
  tmux send-keys -t "$SESSION" t
  sleep 1

  # Close dialog (Escape)
  tmux send-keys -t "$SESSION" Escape
  sleep 0.5

  # Read config to verify persistence
  local config
  config=$(read_config)
  assert_ok "Config contains transparent:true" \
    'echo "$config" | grep -q "\"transparent\":true"'

  teardown_env
}

# ---------- Test 18: Cross-category switch ----------

test_18_cross_category_switch() {
  echo ""
  echo "=== Test 18: Cross-category switch (clean-room → amber-forge) ==="
  setup_env
  write_config '{"options":{"tui":{"theme":"clean-room"}}}'
  launch_crucible

  # Verify app started with clean-room
  local capture
  capture=$(capture_pane)
  assert_ok "App started with clean-room" \
    'echo "$capture" | grep -q "▀"'

  # Open theme dialog (ctrl+y)
  tmux send-keys -t "$SESSION" C-y
  sleep 1

  # clean-room is last (index 5), so it's pre-selected
  # Theme order: steel-blue(0), amber-forge(1), phosphor-green(2), reactor-red(3), titanium(4), clean-room(5)
  # We need amber-forge which is at index 1
  # From clean-room (5), go Up 4 times to reach amber-forge (1)
  tmux send-keys -t "$SESSION" Up Up Up Up
  sleep 0.5

  # Select amber-forge
  tmux send-keys -t "$SESSION" Enter
  sleep 2

  # Capture to verify app still renders after cross-category switch
  local capture_after
  capture_after=$(capture_pane)
  assert_ok "App still renders after switch (text visible)" \
    'echo "$capture_after" | grep -q "▀"'

  # Verify app didn't crash (we can still capture a non-empty pane)
  assert_ok "App didn't crash (non-empty pane)" \
    '[ -n "$capture_after" ]'

  teardown_env
}

# ---------- Test 19: Backward compatibility ----------

test_19_backward_compat() {
  echo ""
  echo "=== Test 19: Backward compatibility (transparent only, no theme) ==="
  setup_env
  # Write config with ONLY transparent:true — no theme specified
  write_config '{"options":{"tui":{"transparent":true}}}'
  launch_crucible

  local capture
  capture=$(capture_pane)

  assert_ok "App starts normally with transparent-only config" \
    'echo "$capture" | grep -q "▀"'

  # Verify no crash — check stderr for panics
  local stderr
  stderr=$(cat "$TEST_DIR/stderr.log" 2>/dev/null || echo "")
  assert_ok "No panic in stderr" \
    '! echo "$stderr" | grep -qi "panic\|fatal\|runtime error"'

  teardown_env
}

# ---------- Run all tests ----------

echo "================================================================"
echo "  Crucible Theme Integration Tests (Acceptance Criteria #13-#19)"
echo "================================================================"

test_13_clean_room
test_14_steel_blue
test_15_transparent_dark
test_16_theme_switch_persistence
test_17_transparent_toggle_persistence
test_18_cross_category_switch
test_19_backward_compat

echo ""
echo "================================================================"
echo "  Results: $PASS passed, $FAIL failed (out of $TOTAL)"
echo "================================================================"

if [ "$FAIL" -gt 0 ]; then
  exit 1
fi
echo "All tests passed!"
