package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image/color"
	"io"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"
)

const (
	historyMax     = 100
	haikusTimeout  = 30 * time.Second
	maxInputLen = 1000
)

type HookInput struct {
	ToolName  string          `json:"tool_name"`
	ToolInput json.RawMessage `json:"tool_input"`
	Cwd       string          `json:"cwd"`
}

type BashToolInput struct {
	Command string `json:"command"`
}

type HaikuResponse struct {
	StructuredOutput struct {
		RiskLevel string `json:"risk_level"`
	} `json:"structured_output"`
}

type HistoryEntry struct {
	Timestamp string `json:"timestamp"`
	ToolName  string `json:"tool_name"`
	ToolInput string `json:"tool_input"`
	Decision  string `json:"decision"`
	RiskLevel string `json:"risk_level"`
}

var (
	cwd                 string
	historyFile         string
	debugLogFile        string
	gcloudReadRe        = regexp.MustCompile(`^gcloud\s+.*\s+(list|describe|get)(\s|$)`)
	alwaysDialogTools   = map[string]bool{"ExitPlanMode": true}
	waitForSIGTERMTools = map[string]bool{"AskUserQuestion": true}
)

func init() {
	home, err := os.UserHomeDir()
	if err != nil {
		log.Fatal(err)
	}
	historyFile = filepath.Join(home, ".claude", "permission-history.jsonl")
	debugLogFile = filepath.Join(home, ".claude", "permission-debug.log")
}

func main() {
	rawInput, err := io.ReadAll(os.Stdin)
	if err != nil {
		log.Fatal(err)
	}

	var input HookInput
	if err := json.Unmarshal(rawInput, &input); err != nil {
		log.Fatal(err)
	}

	toolInput := string(input.ToolInput)
	if len(toolInput) > maxInputLen {
		toolInput = toolInput[:maxInputLen]
	}

	cwd = input.Cwd

	debugLog(input.ToolName, string(rawInput))

	// --- Wait for SIGTERM tools (let Claude Code handle natively) ---
	if waitForSIGTERMTools[input.ToolName] {
		waitForSIGTERM()
	}

	// --- Tools that always require manual approval ---
	if alwaysDialogTools[input.ToolName] {
		showDialog(input.ToolName, toolInput, "manual_required")
	}

	// --- Auto-approve gcloud read-only commands ---
	if input.ToolName == "Bash" {
		var bash BashToolInput
		if err := json.Unmarshal(input.ToolInput, &bash); err == nil {
			if gcloudReadRe.MatchString(bash.Command) {
				approve(input.ToolName, "gcloud_read")
			}
		}
	}

	// --- Risk evaluation via Claude Haiku ---
	riskLevel := evaluateRisk(input.ToolName, toolInput)

	switch riskLevel {
	case "very_low", "low":
		approve(input.ToolName, riskLevel)
	case "medium", "high", "very_high":
		showDialog(input.ToolName, toolInput, riskLevel)
	default:
		showDialog(input.ToolName, toolInput, "unknown")
	}
}

// --- Helper functions ---

func debugLog(toolName, rawInput string) {
	f, err := os.OpenFile(debugLogFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "%s tool_name=%s input=%s\n", time.Now().UTC().Format(time.RFC3339), toolName, rawInput)
}

func waitForSIGTERM() {
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGTERM, syscall.SIGINT)
	<-sig
	os.Exit(0)
}

func outputJSON(behavior string) {
	fmt.Printf(`{"hookSpecificOutput":{"hookEventName":"PermissionRequest","decision":{"behavior":"%s"}}}`, behavior)
}

func approve(toolName, riskLevel string) {
	exec.Command("osascript", "-e",
		fmt.Sprintf(`display notification "Auto-approved (%s): %s" with title "Claude Code"`, riskLevel, toolName),
	).Start()
	outputJSON("allow")
	os.Exit(0)
}

func recordDecision(toolName, toolInput, decision, riskLevel string) {
	truncatedInput := toolInput
	if len(truncatedInput) > 200 {
		truncatedInput = truncatedInput[:200]
	}

	entry := HistoryEntry{
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		ToolName:  toolName,
		ToolInput: truncatedInput,
		Decision:  decision,
		RiskLevel: riskLevel,
	}

	data, err := json.Marshal(entry)
	if err != nil {
		return
	}

	f, err := os.OpenFile(historyFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	f.Write(append(data, '\n'))

	trimHistory()
}

func trimHistory() {
	data, err := os.ReadFile(historyFile)
	if err != nil {
		return
	}
	lines := bytes.Split(bytes.TrimSpace(data), []byte("\n"))
	if len(lines) > historyMax {
		lines = lines[len(lines)-historyMax:]
		os.WriteFile(historyFile, append(bytes.Join(lines, []byte("\n")), '\n'), 0644)
	}
}

// --- Fyne dialog ---

func riskColor(level string) color.Color {
	switch level {
	case "very_low":
		return color.NRGBA{R: 76, G: 175, B: 80, A: 255} // green
	case "low":
		return color.NRGBA{R: 33, G: 150, B: 243, A: 255} // blue
	case "medium":
		return color.NRGBA{R: 255, G: 152, B: 0, A: 255} // orange
	case "high":
		return color.NRGBA{R: 244, G: 67, B: 54, A: 255} // red
	case "very_high":
		return color.NRGBA{R: 183, G: 28, B: 28, A: 255} // dark red
	default:
		return color.NRGBA{R: 117, G: 117, B: 117, A: 255} // gray
	}
}

func showDialog(toolName, toolInput, riskLevel string) {
	a := app.New()
	w := a.NewWindow("Claude Code Permission Request")

	// Risk level bar (full-width colored banner)
	riskDisplayText := strings.ToUpper(strings.ReplaceAll(riskLevel, "_", " "))
	barBg := canvas.NewRectangle(riskColor(riskLevel))
	barBg.SetMinSize(fyne.NewSize(0, 44))
	barText := canvas.NewText(riskDisplayText, color.White)
	barText.TextStyle = fyne.TextStyle{Bold: true}
	barText.TextSize = 18
	barText.Alignment = fyne.TextAlignCenter
	riskBar := container.NewStack(barBg, container.NewCenter(barText))

	// Tool name and cwd
	toolLabel := widget.NewRichTextFromMarkdown("**Tool:** " + toolName)
	cwdLabel := widget.NewRichTextFromMarkdown("**CWD:** " + cwd)

	// Tool input (scrollable)
	inputLabel := widget.NewLabel(toolInput)
	inputLabel.Wrapping = fyne.TextWrapBreak
	inputScroll := container.NewVScroll(inputLabel)
	inputScroll.SetMinSize(fyne.NewSize(0, 120))

	// Keyboard hint
	hint := canvas.NewText("Enter: Approve  /  Escape: Deny", color.NRGBA{R: 140, G: 140, B: 140, A: 255})
	hint.TextSize = 12

	// Buttons (initially disabled to prevent accidental input)
	result := "denied"
	armed := false

	approveBtn := widget.NewButton("Approve", func() {
		if !armed {
			return
		}
		result = "approved"
		a.Quit()
	})
	approveBtn.Importance = widget.HighImportance
	approveBtn.Disable()

	denyBtn := widget.NewButton("Deny", func() {
		if !armed {
			return
		}
		result = "denied"
		a.Quit()
	})
	denyBtn.Disable()

	buttons := container.NewHBox(layout.NewSpacer(), denyBtn, approveBtn)

	// Keyboard shortcuts (ignored until armed)
	w.Canvas().SetOnTypedKey(func(e *fyne.KeyEvent) {
		if !armed {
			return
		}
		switch e.Name {
		case fyne.KeyReturn:
			result = "approved"
			a.Quit()
		case fyne.KeyEscape:
			result = "denied"
			a.Quit()
		}
	})

	// Arm buttons after 500ms to prevent accidental input
	go func() {
		time.Sleep(500 * time.Millisecond)
		fyne.Do(func() {
			armed = true
			approveBtn.Enable()
			denyBtn.Enable()
		})
	}()

	// Layout: risk bar full-width at top, padded content below
	w.SetContent(container.NewBorder(
		// Top: risk bar + tool label
		container.NewVBox(
			riskBar,
			container.NewPadded(container.NewVBox(toolLabel, cwdLabel)),
		),
		// Bottom: hint + buttons
		container.NewPadded(container.NewVBox(
			widget.NewSeparator(),
			hint,
			buttons,
		)),
		nil, nil,
		// Center: scrollable tool input (fills remaining space)
		container.NewPadded(inputScroll),
	))

	w.Resize(fyne.NewSize(520, 400))
	w.CenterOnScreen()
	w.SetFixedSize(true)
	w.ShowAndRun()

	// Process result after dialog closes
	if result == "approved" {
		recordDecision(toolName, toolInput, "approve", riskLevel)
		outputJSON("allow")
	} else {
		recordDecision(toolName, toolInput, "deny", riskLevel)
		outputJSON("deny")
	}
	os.Exit(0)
}

// --- Risk evaluation via Claude Haiku ---

func buildHistoryContext() string {
	data, err := os.ReadFile(historyFile)
	if err != nil {
		return ""
	}

	lines := bytes.Split(bytes.TrimSpace(data), []byte("\n"))
	if len(lines) > historyMax {
		lines = lines[len(lines)-historyMax:]
	}

	var parts []string
	for _, line := range lines {
		if len(line) == 0 {
			continue
		}
		var entry HistoryEntry
		if err := json.Unmarshal(line, &entry); err != nil {
			continue
		}
		input := entry.ToolInput
		if len(input) > 100 {
			input = input[:100]
		}
		parts = append(parts, fmt.Sprintf("- %s: %s %s", entry.Decision, entry.ToolName, input))
	}
	return strings.Join(parts, "\n")
}

func evaluateRisk(toolName, toolInput string) string {
	systemPrompt := `You are a security risk classifier. Classify the risk level of the given tool call. Do NOT use any tools. Respond immediately with the structured output only.

Risk criteria:
- very_low: Read-only, no side effects (ls, cat, git status, git diff, git log, grep, Read, Glob, Grep, LS tools)
- low: Minor side effects, easily reversible (mkdir, cp, git add, git commit, file edits, Write, Edit tools)
- medium: Moderate side effects, network writes (git push (non-force), npm install, pip install, docker run)
- high: Destructive or hard to reverse (rm -rf, git reset --hard, git push --force, DROP TABLE, connections to untrusted internet endpoints)
- very_high: Extremely dangerous (rm -rf /, curl|bash from untrusted URL, sudo on system files)`

	userPrompt := fmt.Sprintf("Tool name: %s\nTool input: %s", toolName, toolInput)

	historyCtx := buildHistoryContext()
	if historyCtx != "" {
		userPrompt += "\n\nRecent manual decisions by the user (approve/deny) for reference:\n" + historyCtx
	}

	jsonSchema := `{"type":"object","properties":{"risk_level":{"type":"string","enum":["very_low","low","medium","high","very_high"]}},"required":["risk_level"]}`

	cmd := exec.Command("claude",
		"--model", "claude-haiku-4-5-20251001",
		"-p", userPrompt,
		"--system-prompt", systemPrompt,
		"--output-format", "json",
		"--json-schema", jsonSchema,
		"--no-session-persistence",
	)
	cmd.Stdin = nil
	cmd.Env = append(os.Environ(), "CLAUDECODE=")

	var out bytes.Buffer
	cmd.Stdout = &out

	if err := cmd.Start(); err != nil {
		showDialog(toolName, toolInput, "error")
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	select {
	case err := <-done:
		if err != nil {
			showDialog(toolName, toolInput, "error")
		}
	case <-time.After(haikusTimeout):
		cmd.Process.Kill()
		showDialog(toolName, toolInput, "timeout")
	}

	var resp HaikuResponse
	if err := json.Unmarshal(out.Bytes(), &resp); err != nil || resp.StructuredOutput.RiskLevel == "" {
		showDialog(toolName, toolInput, "parse_error")
	}

	return resp.StructuredOutput.RiskLevel
}
