package cli

import (
	"github.com/urfave/cli/v2"
)

const (
	forceFlag = "force"
)

func (a *App) removeCommand() *cli.Command {
	return &cli.Command{
		Name:    "remove",
		Aliases: []string{"rm"},
		Usage:   "remove a branch",
		Action:  a.handleRemoveCommand,
		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name: forceFlag,
				Aliases: []string{
					"f",
				},
				Value: false,
			},
		},
		ArgsUsage: "BRANCH",
	}
}

func (a *App) handleRemoveCommand(cctx *cli.Context) error {
	if cctx.Args().Len() != 1 {
		return cli.ShowCommandHelp(cctx, "remove")
	}

	branch := cctx.Args().Get(0)

	worktrees, err := a.git.Worktrees()
	if err != nil {
		return err
	}

	for _, worktree := range worktrees {
		if worktree.Branch == "refs/heads/"+branch {
			if err := a.git.RemoveWorktree(worktree.Dir); err != nil {
				return err
			}
			break
		}
	}

	if err := a.git.DeleteBranch(branch, cctx.Bool(forceFlag)); err != nil {
		return err
	}

	return nil
}
