package main

import (
	"log"
	"os"

	"github.com/ryotarai/misc/gg/cli"
	"github.com/ryotarai/misc/gg/git"
)

func main() {
	app, err := cli.New(git.New("git"))
	if err != nil {
		log.Fatal(err)
	}

	if err := app.Run(os.Args); err != nil {
		log.Fatal(err)
	}
}
