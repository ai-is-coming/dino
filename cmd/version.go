package cmd

import (
	"fmt"

	"github.com/ai-is-coming/dino/internal/conf"

	"github.com/fatih/color"
	"github.com/spf13/cobra"
)

var (
	Version   = "v0.0.0"
	GitCommit = ""
	BuildTime = ""
)

// versionCmd represents the version command.
var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the version number",
	Long:  fmt.Sprintf("All software has versions. This is %s's", conf.AppName),
	Run: func(cmd *cobra.Command, args []string) {
		color.New(color.FgGreen, color.Bold).Printf("%s %s\n", conf.AppName, Version)
		color.New(color.FgHiBlack).Printf("commit: %s\n", GitCommit)
		color.New(color.FgHiBlack).Printf("build at: %s\n", BuildTime)
	},
}
