package main

import (
	"galal-hussein/cattle-drive/cli/cmds"
	"galal-hussein/cattle-drive/cli/cmds/status"
	"os"

	"github.com/urfave/cli/v2"
)

const (
	program   = "cattle-drive"
	version   = "dev"
	gitCommit = "HEAD"
)

func main() {
	app := cmds.NewApp()
	app.Commands = []*cli.Command{
		status.NewCommand(),
	}
	app.Version = version + " (" + gitCommit + ")"

	if err := app.Run(os.Args); err != nil {
		os.Exit(1)
	}
}
