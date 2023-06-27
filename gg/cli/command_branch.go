package cli

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/urfave/cli/v2"
)

const (
	branchPrefixFlag = "branch-prefix"
	openFlag         = "open"
)

func (a *App) newCommand() (*cli.Command, error) {
	return &cli.Command{
		Name:    "new",
		Aliases: []string{"b"},
		Usage:   "create a new branch",
		Action:  a.handleNewCommand,
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name: branchPrefixFlag,
				EnvVars: []string{
					"GG_BRANCH_PREFIX",
				},
			},
			&cli.StringFlag{
				Name: openFlag,
				EnvVars: []string{
					"GG_OPEN",
				},
			},
		},
	}, nil
}

func (a *App) handleNewCommand(cctx *cli.Context) error {
	defaultBranch, err := a.git.DefaultBranch()
	if err != nil {
		return err
	}

	rootDir, err := a.git.RootDir()
	if err != nil {
		return err
	}

	reader := bufio.NewReader(os.Stdin)
	fmt.Print("Enter title: ")
	title, err := reader.ReadString('\n')
	if err != nil {
		return err
	}
	title = strings.TrimSpace(title)

	branch := cctx.String(branchPrefixFlag) + strings.TrimPrefix(regexp.MustCompile(`[^\w]+`).ReplaceAllString(strings.ToLower(title), "-"), "-")
	fmt.Printf("Creating a new branch: %s\n", branch)

	dir := filepath.Join(rootDir, "..", filepath.Base(rootDir)+"-"+strings.ReplaceAll(branch, "/", "-"))

	if err := a.git.CreateBranch(branch, defaultBranch); err != nil {
		return err
	}
	if err := a.git.WorktreeAdd(dir, branch); err != nil {
		return err
	}
	if err := a.git.SetTitle(branch, title); err != nil {
		return err
	}

	fmt.Println(dir)

	if openCmd := cctx.String(openFlag); openCmd != "" {
		if err := exec.Command(openCmd, dir).Run(); err != nil {
			return err
		}
	}

	return nil
}
