package cli

import (
	"github.com/ryotarai/misc/gg/git"
	"github.com/urfave/cli/v2"
)

type App struct {
	git *git.Git
}

func New(git *git.Git) (*App, error) {
	return &App{
		git: git,
	}, nil
}

func (a *App) Run(args []string) error {
	newCommand, err := a.newCommand()
	if err != nil {
		return err
	}

	app := &cli.App{
		Usage: "git wrapper",
		Commands: []*cli.Command{
			newCommand,
			a.removeCommand(),
			a.prCommand(),
		},
	}

	return app.Run(args)
}
