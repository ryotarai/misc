package cli

import (
	"os/exec"

	"github.com/urfave/cli/v2"
)

func (a *App) prCommand() *cli.Command {
	return &cli.Command{
		Name:   "pr",
		Usage:  "create a pull request",
		Action: a.handlePRCommand,
		Flags:  []cli.Flag{},
	}
}

func (a *App) handlePRCommand(cctx *cli.Context) error {
	currentBranch, err := a.git.CurrentBranch()
	if err != nil {
		return err
	}

	// if dirty, commit

	// push to origin
	if err := a.git.Push(currentBranch); err != nil {
		return err
	}

	// gh pr create
	title, err := a.git.GetTitle(currentBranch)
	if err != nil {
		return err
	}

	if err := exec.Command("gh", "pr", "create", "--web", "--title", title).Run(); err != nil {
		return err
	}

	return nil
}
