package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
	"time"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	timeoutStr := flag.String("timeout", "1h", "timeout")
	flag.Parse()
	args := flag.Args()
	if len(args) != 1 {
		return fmt.Errorf("usage: waitpr URL")
	}
	pr := args[0]
	if !strings.HasPrefix(pr, "https://github.com/") {
		return fmt.Errorf("usage: waitpr URL")
	}

	timeout, err := time.ParseDuration(*timeoutStr)
	if err != nil {
		return err
	}

	if err := waitPR(pr, timeout); err != nil {
		return err
	}

	if err := exec.Command("open", pr).Run(); err != nil {
		return err
	}

	if err := exec.Command("terminal-notifier", "-title", "CI finished", "-message", pr, "-sound", "Blow", "-open", pr).Run(); err != nil {
		return err
	}

	return nil
}

func waitPR(pr string, timeout time.Duration) error {
	startAt := time.Now()
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		var output StatusCheckRollupOutput
		if err := commandJSON(&output, "gh", "pr", "view", "--json=statusCheckRollup", pr); err != nil {
			return err
		}
		var completed int
		for _, check := range output.StatusChecks {
			switch check.TypeName {
			case "CheckRun":
				if check.Status == "COMPLETED" {
					completed++
				}
			case "StatusContext":
				if check.State == "ERROR" || check.State == "FAILURE" || check.State == "SUCCESS" {
					completed++
				}
			default:
				return fmt.Errorf("unexpected type: %s", check.TypeName)
			}
		}
		log.Printf("%d/%d completed", completed, len(output.StatusChecks))
		if completed == len(output.StatusChecks) {
			return nil
		}
		if timeout < time.Since(startAt) {
			return fmt.Errorf("timeout")
		}
		<-ticker.C
	}
}

func commandJSON(v interface{}, name string, args ...string) error {
	stdout := new(bytes.Buffer)
	cmd := exec.Command(name, args...)
	cmd.Stderr = os.Stderr
	cmd.Stdout = stdout
	cmd.Run()

	return json.Unmarshal(stdout.Bytes(), v)
}

type StatusCheckRollupOutput struct {
	StatusChecks []StatusCheck `json:"statusCheckRollup"`
}

type StatusCheck struct {
	TypeName string `json:"__typename"`

	// common fields
	StartedAt string `json:"startedAt"`

	// TypeName == CheckRun
	// https://docs.github.com/en/graphql/reference/objects#checkrun
	CompletedAt  string `json:"completedAt"`
	Conclusion   string `json:"conclusion"` // https://docs.github.com/en/graphql/reference/enums#checkconclusionstate
	DetailsUrl   string `json:"detailsUrl"`
	Name         string `json:"name"`
	Status       string `json:"status"` // https://docs.github.com/en/graphql/reference/enums#checkstatusstate
	WorkflowName string `json:"workflowName"`

	// TypeName == StatusContext
	// https://docs.github.com/en/graphql/reference/objects#statuscontext
	Context   string `json:"context"`
	State     string `json:"state"` // https://docs.github.com/en/graphql/reference/enums#statusstate
	TargetUrl string `json:"targetUrl"`
}
