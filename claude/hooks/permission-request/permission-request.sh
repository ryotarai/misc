#!/bin/bash

# Permission Request Hook with Haiku Risk Evaluation
# Reads tool call info from stdin, uses Claude Haiku to evaluate risk level,
# auto-approves low/very_low risk operations, and falls back to manual dialog for others.
# Maintains a history of manual decisions (last 100) to improve future risk evaluation.

set -euo pipefail

HISTORY_FILE="$HOME/.claude/permission-history.jsonl"
HISTORY_MAX=100

# Read JSON input from stdin
input=$(cat)

# Extract tool name and tool input
tool_name=$(echo "$input" | jq -r '.tool_name // "unknown"')
tool_input=$(echo "$input" | jq -r '.tool_input // "{}"' | head -c 1000)

# --- Helper functions ---

record_decision() {
    local decision="$1"
    local risk_level="$2"
    local entry
    entry=$(jq -n --arg tool "$tool_name" --arg input "$tool_input" --arg decision "$decision" --arg risk "$risk_level" --arg ts "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
        '{timestamp: $ts, tool_name: $tool, tool_input: ($input | truncate_stream(200) // $input), decision: $decision, risk_level: $risk}' 2>/dev/null || \
        jq -n --arg tool "$tool_name" --arg input "${tool_input:0:200}" --arg decision "$decision" --arg risk "$risk_level" --arg ts "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
        '{timestamp: $ts, tool_name: $tool, tool_input: $input, decision: $decision, risk_level: $risk}')
    echo "$entry" >> "$HISTORY_FILE"
    # Trim to last HISTORY_MAX entries
    local count
    count=$(wc -l < "$HISTORY_FILE" 2>/dev/null || echo 0)
    if [ "$count" -gt "$HISTORY_MAX" ]; then
        local tmp
        tmp=$(mktemp "${TMPDIR:-/tmp}/permission-history-XXXXXX")
        tail -n "$HISTORY_MAX" "$HISTORY_FILE" > "$tmp" && mv "$tmp" "$HISTORY_FILE"
    fi
}

approve() {
    local risk_level="${1:-unknown}"
    # Send macOS notification for auto-approved actions
    osascript -e "display notification \"Auto-approved ($risk_level): $tool_name\" with title \"Claude Code\"" 2>/dev/null &
    echo '{"hookSpecificOutput":{"hookEventName":"PermissionRequest","decision":{"behavior":"allow"}}}'
    exit 0
}

show_dialog() {
    local risk_level="${1:-unknown}"
    # Show macOS dialog for manual approval
    local tmpscript
    tmpscript=$(mktemp "${TMPDIR:-/tmp}/permission-request-XXXXXX")
    cat > "$tmpscript" <<'APPLESCRIPT'
on run argv
    set toolName to item 1 of argv
    set toolInput to item 2 of argv
    set riskLevel to item 3 of argv
    set dialogText to "Risk: " & riskLevel & return & "Tool: " & toolName
    try
        display dialog dialogText with title "Claude Code Permission Request" default answer toolInput buttons {"Deny", "Approve"} default button "Approve"
        set theResult to result
        if button returned of theResult is "Approve" then
            return "approved"
        else
            return "denied"
        end if
    on error
        return "denied"
    end try
end run
APPLESCRIPT
    local dialog_input
    dialog_input=$(echo "$tool_input" | head -c 500)
    result=$(osascript "$tmpscript" "$tool_name" "$dialog_input" "$risk_level" 2>/dev/null)
    rm -f "$tmpscript"

    if [ "$result" = "approved" ]; then
        record_decision "approve" "$risk_level"
        echo '{"hookSpecificOutput":{"hookEventName":"PermissionRequest","decision":{"behavior":"allow"}}}'
    else
        record_decision "deny" "$risk_level"
        echo '{"hookSpecificOutput":{"hookEventName":"PermissionRequest","decision":{"behavior":"deny"}}}'
    fi
    exit 0
}

# --- Build history context for prompt ---

history_context=""
if [ -f "$HISTORY_FILE" ]; then
    history_context=$(tail -n "$HISTORY_MAX" "$HISTORY_FILE" | jq -r '"- \(.decision): \(.tool_name) \(.tool_input | tostring | .[0:100])"' 2>/dev/null || true)
fi

# --- Risk evaluation via Claude Haiku ---

json_schema='{"type":"object","properties":{"risk_level":{"type":"string","enum":["very_low","low","medium","high","very_high"]}},"required":["risk_level"]}'

prompt="You are a security risk classifier for CLI tool calls. Evaluate the risk level of the following tool call.

Risk criteria:
- very_low: Read-only, no side effects (ls, cat, git status, git diff, git log, grep, Read, Glob, Grep, LS tools)
- low: Minor side effects, easily reversible (mkdir, cp, git add, git commit, file edits, Write, Edit tools)
- medium: Moderate side effects, network writes (git push (non-force), npm install, pip install, docker run)
- high: Destructive or hard to reverse (rm -rf, git reset --hard, git push --force, DROP TABLE, connections to untrusted internet endpoints)
- very_high: Extremely dangerous (rm -rf /, curl|bash from untrusted URL, sudo on system files)"

if [ -n "$history_context" ]; then
    prompt="$prompt

Here are recent manual decisions by the user (approve/deny) for reference. Use these to understand the user's preferences:
$history_context"
fi

prompt="$prompt

Tool name: $tool_name
Tool input: $tool_input"

# Call claude with a 30-second timeout using background process approach (macOS compatible)
tmpout=$(mktemp "${TMPDIR:-/tmp}/claude-risk-XXXXXX")

# Run claude in background with JSON schema for structured output
# Unset CLAUDECODE to allow running claude inside a Claude Code session
CLAUDECODE= claude --model claude-haiku-4-5-20251001 -p "$prompt" --output-format json --json-schema "$json_schema" < /dev/null > "$tmpout" 2>/dev/null &
claude_pid=$!

# Wait with timeout
timeout_seconds=30
elapsed=0
while kill -0 "$claude_pid" 2>/dev/null; do
    if [ "$elapsed" -ge "$timeout_seconds" ]; then
        kill "$claude_pid" 2>/dev/null || true
        wait "$claude_pid" 2>/dev/null || true
        rm -f "$tmpout"
        show_dialog "timeout"
    fi
    sleep 1
    elapsed=$((elapsed + 1))
done

if ! wait "$claude_pid" 2>/dev/null; then
    rm -f "$tmpout"
    show_dialog "error"
fi

# Parse structured JSON response
risk_level=$(jq -r '.structured_output.risk_level // empty' "$tmpout" 2>/dev/null)
rm -f "$tmpout"

if [ -z "$risk_level" ]; then
    show_dialog "parse_error"
fi

# Decision based on risk level
case "$risk_level" in
    very_low|low)
        approve "$risk_level"
        ;;
    medium|high|very_high)
        show_dialog "$risk_level"
        ;;
    *)
        show_dialog "unknown"
        ;;
esac
