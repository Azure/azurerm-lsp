package main

import (
	"os"

	"github.com/Azure/azurerm-lsp/internal/cmd"
	"github.com/Azure/azurerm-lsp/provider-schema"
	"github.com/mitchellh/cli"
)

func main() {
	if len(os.Args) > 2 && os.Args[1] == "generate" {
		providerPath := os.Args[2]

		gitBranch := "main"
		if len(os.Args) > 3 {
			gitBranch = os.Args[3]
		}

		provider_schema.GenerateProviderSchema(providerPath, gitBranch)
		return
	}

	c := &cli.CLI{
		Name:       "azurerm-lsp",
		Version:    VersionString(),
		Args:       os.Args[1:],
		HelpWriter: os.Stdout,
	}

	ui := &cli.ColoredUi{
		ErrorColor: cli.UiColorRed,
		WarnColor:  cli.UiColorYellow,
		Ui: &cli.BasicUi{
			Writer:      os.Stdout,
			Reader:      os.Stdin,
			ErrorWriter: os.Stderr,
		},
	}

	c.Commands = map[string]cli.CommandFactory{
		"completion": func() (cli.Command, error) {
			return &cmd.CompletionCommand{
				Ui: ui,
			}, nil
		},
		"serve": func() (cli.Command, error) {
			return &cmd.ServeCommand{
				Ui:      ui,
				Version: VersionString(),
			}, nil
		},
		"version": func() (cli.Command, error) {
			return &cmd.VersionCommand{
				Ui:      ui,
				Version: VersionString(),
			}, nil
		},
	}

	exitStatus, err := c.Run()
	if err != nil {
		ui.Error("Error: " + err.Error())
	}

	os.Exit(exitStatus)
}
