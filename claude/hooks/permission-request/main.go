package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image/color"
	"io"
	"log"
	"net/http"
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
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"
)

const (
	historyMax    = 100
	haikusTimeout = 30 * time.Second
	maxInputLen   = 1000
	armDelay      = 500 * time.Millisecond
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

// OpenAI-compatible API types
type chatRequest struct {
	Model          string          `json:"model"`
	Messages       []chatMessage   `json:"messages"`
	ResponseFormat *responseFormat  `json:"response_format,omitempty"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type responseFormat struct {
	Type       string      `json:"type"`
	JSONSchema *jsonSchema `json:"json_schema,omitempty"`
}

type jsonSchema struct {
	Name   string          `json:"name"`
	Schema json.RawMessage `json:"schema"`
}

type chatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

type riskResult struct {
	RiskLevel string `json:"risk_level"`
}

type Config struct {
	APIBase string `json:"api_base"`
	APIKey  string `json:"api_key"`
	Model   string `json:"model"`
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
	config              Config
	gcloudReadRe        = regexp.MustCompile(`^gcloud\s+.*\s+(list|describe|get)(\s|$)`)
	alwaysDialogTools   = map[string]bool{"ExitPlanMode": true}
	waitForSIGTERMTools = map[string]bool{"AskUserQuestion": true}
)

func loadConfig(path string) Config {
	var cfg Config
	data, err := os.ReadFile(path)
	if err != nil {
		return cfg
	}
	json.Unmarshal(data, &cfg)
	return cfg
}

func init() {
	home, err := os.UserHomeDir()
	if err != nil {
		log.Fatal(err)
	}
	historyFile = filepath.Join(home, ".claude", "permission-history.jsonl")
	debugLogFile = filepath.Join(home, ".claude", "permission-debug.log")
	config = loadConfig(filepath.Join(home, ".config", "ryotarai-permission-request", "config.json"))
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

	// --- Auto-approve gcloud read-only commands ---
	if input.ToolName == "Bash" {
		var bash BashToolInput
		if err := json.Unmarshal(input.ToolInput, &bash); err == nil {
			if gcloudReadRe.MatchString(bash.Command) {
				approveImmediate(input.ToolName, "gcloud_read")
			}
		}
	}

	// --- Tools that always require manual approval (skip Haiku) ---
	if alwaysDialogTools[input.ToolName] {
		showDialog(input.ToolName, toolInput, "manual_required", false)
	}

	// --- Show dialog immediately, evaluate risk in background ---
	showDialog(input.ToolName, toolInput, "", true)
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

func approveImmediate(toolName, riskLevel string) {
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

func formatToolInput(raw string) string {
	var obj map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &obj); err != nil {
		return raw
	}

	var lines []string
	for key, val := range obj {
		switch v := val.(type) {
		case string:
			lines = append(lines, fmt.Sprintf("%s: %s", key, v))
		default:
			b, _ := json.Marshal(v)
			lines = append(lines, fmt.Sprintf("%s: %s", key, string(b)))
		}
	}
	return strings.Join(lines, "\n")
}

type EditToolInput struct {
	FilePath   string `json:"file_path"`
	OldString  string `json:"old_string"`
	NewString  string `json:"new_string"`
	ReplaceAll bool   `json:"replace_all"`
}

func computeDiff(oldStr, newStr string) string {
	oldFile, err := os.CreateTemp("", "diff-old-*")
	if err != nil {
		return "- " + oldStr + "\n+ " + newStr
	}
	defer os.Remove(oldFile.Name())

	newFile, err := os.CreateTemp("", "diff-new-*")
	if err != nil {
		return "- " + oldStr + "\n+ " + newStr
	}
	defer os.Remove(newFile.Name())

	oldFile.WriteString(oldStr)
	oldFile.Close()
	newFile.WriteString(newStr)
	newFile.Close()

	out, _ := exec.Command("diff", "-u", oldFile.Name(), newFile.Name()).Output()
	// diff exits 1 when files differ, that's expected
	return string(out)
}

func formatEditDiff(raw string) fyne.CanvasObject {
	var edit EditToolInput
	if err := json.Unmarshal([]byte(raw), &edit); err != nil {
		label := widget.NewLabel(raw)
		label.Wrapping = fyne.TextWrapBreak
		return label
	}

	monoStyle := fyne.TextStyle{Monospace: true}
	red := color.NRGBA{R: 200, G: 50, B: 50, A: 255}
	green := color.NRGBA{R: 50, G: 160, B: 50, A: 255}
	gray := color.NRGBA{R: 100, G: 100, B: 100, A: 255}
	white := color.NRGBA{R: 180, G: 180, B: 180, A: 255}

	var objects []fyne.CanvasObject

	// File path header
	filePath := canvas.NewText(edit.FilePath, gray)
	filePath.TextStyle = monoStyle
	filePath.TextSize = 13
	objects = append(objects, filePath)

	if edit.ReplaceAll {
		replaceAll := canvas.NewText("(replace all)", gray)
		replaceAll.TextSize = 12
		objects = append(objects, replaceAll)
	}

	objects = append(objects, widget.NewSeparator())

	// Run diff -u and parse output
	diffOutput := computeDiff(edit.OldString, edit.NewString)
	for _, line := range strings.Split(diffOutput, "\n") {
		// Skip unified diff header lines
		if strings.HasPrefix(line, "---") || strings.HasPrefix(line, "+++") {
			continue
		}

		var c color.Color
		switch {
		case strings.HasPrefix(line, "-"):
			c = red
		case strings.HasPrefix(line, "+"):
			c = green
		case strings.HasPrefix(line, "@@"):
			c = gray
		default:
			c = white
		}

		t := canvas.NewText(line, c)
		t.TextStyle = monoStyle
		t.TextSize = 13
		objects = append(objects, t)
	}

	return container.NewVBox(objects...)
}

func riskDisplayText(level string) string {
	if level == "" {
		return "EVALUATING..."
	}
	return strings.ToUpper(strings.ReplaceAll(level, "_", " "))
}

func showDialog(toolName, toolInput, initialRiskLevel string, evaluate bool) {
	a := app.New()
	w := a.NewWindow("Claude Code Permission Request")

	currentRiskLevel := initialRiskLevel

	// Risk level bar (full-width colored banner)
	barBg := canvas.NewRectangle(riskColor(currentRiskLevel))
	barBg.SetMinSize(fyne.NewSize(0, 44))
	barText := canvas.NewText(riskDisplayText(currentRiskLevel), color.White)
	barText.TextStyle = fyne.TextStyle{Bold: true}
	barText.TextSize = 18
	barText.Alignment = fyne.TextAlignCenter
	riskBar := container.NewStack(barBg, container.NewCenter(barText))

	// Tool name and cwd (monospace)
	monoStyle := fyne.TextStyle{Monospace: true}
	toolText := widget.NewLabel("Tool: " + toolName)
	toolText.TextStyle = monoStyle
	cwdText := widget.NewLabel("CWD:  " + cwd)
	cwdText.TextStyle = monoStyle
	cwdText.Wrapping = fyne.TextWrapBreak

	// Tool input (scrollable, monospace)
	var inputWidget fyne.CanvasObject
	if toolName == "Edit" {
		inputWidget = formatEditDiff(toolInput)
	} else {
		label := widget.NewLabel(formatToolInput(toolInput))
		label.Wrapping = fyne.TextWrapBreak
		label.TextStyle = monoStyle
		inputWidget = label
	}
	inputScroll := container.NewVScroll(inputWidget)
	inputScroll.SetMinSize(fyne.NewSize(0, 120))

	// Keyboard hint
	hint := canvas.NewText("Cmd+Shift+Enter: Approve  /  Escape: Deny", color.NRGBA{R: 140, G: 140, B: 140, A: 255})
	hint.TextSize = 12

	// Model name label (shown from the start if evaluating)
	evalModelName := ""
	if evaluate {
		evalModelName = riskEvalModelName()
	}
	modelText := canvas.NewText("", color.NRGBA{R: 120, G: 120, B: 120, A: 255})
	modelText.TextSize = 11
	if evalModelName != "" {
		modelText.Text = "eval: " + evalModelName
	}

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
	// Cmd+Shift+Enter to approve
	w.Canvas().AddShortcut(&desktop.CustomShortcut{
		KeyName:  fyne.KeyReturn,
		Modifier: fyne.KeyModifierSuper | fyne.KeyModifierShift,
	}, func(_ fyne.Shortcut) {
		if !armed {
			return
		}
		result = "approved"
		a.Quit()
	})
	// Escape to deny
	w.Canvas().SetOnTypedKey(func(e *fyne.KeyEvent) {
		if !armed {
			return
		}
		if e.Name == fyne.KeyEscape {
			result = "denied"
			a.Quit()
		}
	})

	// Close dialog on SIGTERM/SIGINT
	go func() {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, syscall.SIGTERM, syscall.SIGINT)
		<-sig
		a.Quit()
	}()

	// Arm buttons after delay
	go func() {
		time.Sleep(armDelay)
		fyne.Do(func() {
			armed = true
			approveBtn.Enable()
			denyBtn.Enable()
		})
	}()

	// Evaluate risk in background
	if evaluate {
		go func() {
			evalResult := evaluateRisk(toolName, toolInput)
			fyne.Do(func() {
				currentRiskLevel = evalResult.riskLevel
				barBg.FillColor = riskColor(evalResult.riskLevel)
				barText.Text = riskDisplayText(evalResult.riskLevel)
				barBg.Refresh()
				barText.Refresh()

				if evalResult.riskLevel == "very_low" || evalResult.riskLevel == "low" {
					result = "approved"
					a.Quit()
				}
			})
		}()
	}

	// Layout
	w.SetContent(container.NewBorder(
		container.NewVBox(
			riskBar,
			container.NewPadded(container.NewVBox(toolText, cwdText)),
		),
		container.NewPadded(container.NewVBox(
			widget.NewSeparator(),
			container.NewHBox(hint, layout.NewSpacer(), modelText),
			buttons,
		)),
		nil, nil,
		container.NewPadded(inputScroll),
	))

	w.Resize(fyne.NewSize(520, 400))
	w.SetFixedSize(true)

	// Show window, position at bottom-right, then run event loop
	w.Show()
	moveToBottomRight()
	a.Run()

	// Process result after dialog closes
	if result == "approved" {
		recordDecision(toolName, toolInput, "approve", currentRiskLevel)
		outputJSON("allow")
	} else {
		recordDecision(toolName, toolInput, "deny", currentRiskLevel)
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

var riskSystemPrompt = `You are a security risk classifier. Classify the risk level of the given tool call. Do NOT use any tools. Respond immediately with the structured output only.

Risk criteria:
- very_low: Read-only, no side effects (ls, cat, git status, git diff, git log, grep, Read, Glob, Grep, LS tools)
- low: Minor side effects, easily reversible (mkdir, cp, git add, git commit, file edits, Write, Edit tools)
- medium: Moderate side effects, network writes (git push (non-force), npm install, pip install, docker run)
- high: Destructive or hard to reverse (rm -rf, git reset --hard, git push --force, DROP TABLE, connections to untrusted internet endpoints)
- very_high: Extremely dangerous (rm -rf /, curl|bash from untrusted URL, sudo on system files)`

var riskJSONSchema = json.RawMessage(`{"type":"object","properties":{"risk_level":{"type":"string","enum":["very_low","low","medium","high","very_high"]}},"required":["risk_level"]}`)

func buildUserPrompt(toolName, toolInput string) string {
	userPrompt := fmt.Sprintf("Tool name: %s\nTool input: %s", toolName, toolInput)
	historyCtx := buildHistoryContext()
	if historyCtx != "" {
		userPrompt += "\n\nRecent manual decisions by the user (approve/deny) for reference:\n" + historyCtx
	}
	return userPrompt
}

func getConfigValue(envKey, configValue, defaultValue string) string {
	if v := os.Getenv(envKey); v != "" {
		return v
	}
	if configValue != "" {
		return configValue
	}
	return defaultValue
}

type riskEvalResult struct {
	riskLevel string
	modelName string
}

func riskEvalModelName() string {
	apiKey := getConfigValue("RISK_EVAL_API_KEY", config.APIKey, "")
	if apiKey == "" {
		apiKey = os.Getenv("GEMINI_API_KEY")
	}
	if apiKey == "" {
		return "Claude Code"
	}
	return getConfigValue("RISK_EVAL_MODEL", config.Model, "gemini-2.0-flash")
}

func evaluateRisk(toolName, toolInput string) riskEvalResult {
	apiKey := getConfigValue("RISK_EVAL_API_KEY", config.APIKey, "")
	if apiKey == "" {
		apiKey = os.Getenv("GEMINI_API_KEY")
	}
	if apiKey == "" {
		return evaluateRiskCLI(toolName, toolInput)
	}
	return evaluateRiskAPI(toolName, toolInput, apiKey)
}

func evaluateRiskAPI(toolName, toolInput, apiKey string) riskEvalResult {
	apiBase := getConfigValue("RISK_EVAL_API_BASE", config.APIBase, "https://generativelanguage.googleapis.com/v1beta/openai")
	model := getConfigValue("RISK_EVAL_MODEL", config.Model, "gemini-2.0-flash")
	endpoint := apiBase + "/chat/completions"
	errResult := riskEvalResult{riskLevel: "error", modelName: model}

	debugLog("evaluateRiskAPI", fmt.Sprintf("endpoint=%s model=%s", endpoint, model))

	reqBody := chatRequest{
		Model: model,
		Messages: []chatMessage{
			{Role: "system", Content: riskSystemPrompt},
			{Role: "user", Content: buildUserPrompt(toolName, toolInput)},
		},
		ResponseFormat: &responseFormat{
			Type: "json_schema",
			JSONSchema: &jsonSchema{
				Name:   "risk",
				Schema: riskJSONSchema,
			},
		},
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		debugLog("evaluateRiskAPI", fmt.Sprintf("marshal error: %v", err))
		return errResult
	}

	req, err := http.NewRequest("POST", endpoint, bytes.NewReader(bodyBytes))
	if err != nil {
		debugLog("evaluateRiskAPI", fmt.Sprintf("new request error: %v", err))
		return errResult
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	client := &http.Client{Timeout: haikusTimeout}
	resp, err := client.Do(req)
	if err != nil {
		debugLog("evaluateRiskAPI", fmt.Sprintf("http error: %v", err))
		return errResult
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		debugLog("evaluateRiskAPI", fmt.Sprintf("read body error: %v", err))
		return errResult
	}

	if resp.StatusCode != http.StatusOK {
		debugLog("evaluateRiskAPI", fmt.Sprintf("status=%d body=%s", resp.StatusCode, string(respBody)))
		return errResult
	}

	var chatResp chatResponse
	if err := json.Unmarshal(respBody, &chatResp); err != nil {
		debugLog("evaluateRiskAPI", fmt.Sprintf("unmarshal response error: %v body=%s", err, string(respBody)))
		errResult.riskLevel = "parse_error"
		return errResult
	}

	if len(chatResp.Choices) == 0 {
		debugLog("evaluateRiskAPI", fmt.Sprintf("no choices in response: %s", string(respBody)))
		errResult.riskLevel = "parse_error"
		return errResult
	}

	content := chatResp.Choices[0].Message.Content
	var result riskResult
	if err := json.Unmarshal([]byte(content), &result); err != nil || result.RiskLevel == "" {
		debugLog("evaluateRiskAPI", fmt.Sprintf("unmarshal content error: %v content=%s", err, content))
		errResult.riskLevel = "parse_error"
		return errResult
	}

	debugLog("evaluateRiskAPI", fmt.Sprintf("result=%s", result.RiskLevel))
	return riskEvalResult{riskLevel: result.RiskLevel, modelName: model}
}

func evaluateRiskCLI(toolName, toolInput string) riskEvalResult {
	cliModel := "claude-haiku-4-5-20251001"
	errResult := riskEvalResult{riskLevel: "error", modelName: "Claude Code"}
	userPrompt := buildUserPrompt(toolName, toolInput)

	cmd := exec.Command("claude",
		"--model", cliModel,
		"-p", userPrompt,
		"--system-prompt", riskSystemPrompt,
		"--output-format", "json",
		"--json-schema", string(riskJSONSchema),
		"--no-session-persistence",
	)
	cmd.Stdin = nil
	cmd.Env = append(os.Environ(), "CLAUDECODE=")

	var out bytes.Buffer
	cmd.Stdout = &out

	if err := cmd.Start(); err != nil {
		return errResult
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	select {
	case err := <-done:
		if err != nil {
			return errResult
		}
	case <-time.After(haikusTimeout):
		cmd.Process.Kill()
		errResult.riskLevel = "timeout"
		return errResult
	}

	var resp HaikuResponse
	if err := json.Unmarshal(out.Bytes(), &resp); err != nil || resp.StructuredOutput.RiskLevel == "" {
		errResult.riskLevel = "parse_error"
		return errResult
	}

	return riskEvalResult{riskLevel: resp.StructuredOutput.RiskLevel, modelName: "Claude Code"}
}
