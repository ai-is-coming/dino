package cmd

import (
	"os"

	"github.com/ai-is-coming/dino/internal/conf"

	"github.com/fatih/color"
	"github.com/spf13/cobra"
)

var (
	cfgFile string
	verbose bool
)

// rootCmd represents the base command when called without any subcommands.
var rootCmd = &cobra.Command{
	Use:   conf.AppName,
	Short: "A dino tool",
	Long:  "A dino tool",
}

func attachRootFlags() {
	rootCmd.PersistentFlags().StringVarP(
		&cfgFile,
		"config", "c", "",
		"config file path (default: ./conf.yaml or dino/conf.yaml)",
	)
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "verbose output")
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {
	// Initialize configuration before executing commands.
	cobra.OnInitialize(initConfig)

	// Configure flags without using init() to satisfy linters.
	attachRootFlags()
	attachRunFlags()
	attachConfFlags()

	// Register subcommands.
	rootCmd.AddCommand(versionCmd)
	rootCmd.AddCommand(runCmd)
	rootCmd.AddCommand(confCmd)

	if err := rootCmd.Execute(); err != nil {
		color.New(color.FgRed, color.Bold).Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// initConfig reads in config file and ENV variables if set.
func initConfig() {
	// Initialize koanf configuration
	if err := conf.Init(cfgFile); err != nil {
		if verbose {
			color.New(color.FgYellow).Fprintf(os.Stderr, "Warning: error loading config: %v\n", err)
		}
	} else {
		if verbose && cfgFile != "" {
			color.New(color.FgGreen).Fprintf(os.Stderr, "Using config file: %s\n", cfgFile)
		}
	}
}
