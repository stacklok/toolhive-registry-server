// Package app provides the entry point for the ToolHive Registry API application.
package app

import (
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/stacklok/toolhive-registry-server/internal/versions"
)

var rootCmd = &cobra.Command{
	Use:               "thv-registry-api",
	DisableAutoGenTag: true,
	Short:             "ToolHive Registry API server",
	Long: `ToolHive Registry API server provides REST endpoints for accessing MCP server registry
data in Kubernetes.`,
	Run: func(cmd *cobra.Command, _ []string) {
		// If no subcommand is provided, print help
		if err := cmd.Help(); err != nil {
			slog.Error("Error displaying help", "error", err)
		}
	},
}

// NewRootCmd creates a new root command for the Registry API.
func NewRootCmd() *cobra.Command {
	// Add persistent flags
	rootCmd.PersistentFlags().Bool("debug", false, "Enable debug mode")
	err := viper.BindPFlag("debug", rootCmd.PersistentFlags().Lookup("debug"))
	if err != nil {
		slog.Error("Error binding debug flag", "error", err)
	}

	// Add subcommands
	rootCmd.AddCommand(serveCmd)
	rootCmd.AddCommand(versionCmd)
	rootCmd.AddCommand(migrateCmd)
	rootCmd.AddCommand(primeDbCmd)

	return rootCmd
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print version information",
	Run: func(cmd *cobra.Command, _ []string) {
		info := versions.GetVersionInfo()
		format, err := cmd.Flags().GetString("format")
		if err != nil {
			slog.Error("Error retrieving format flag", "error", err)
			return
		}

		if format == "json" {
			output, err := json.MarshalIndent(info, "", "  ")
			if err != nil {
				slog.Error("Error formatting version info as JSON", "error", err)
				return
			}
			fmt.Println(string(output))
		} else {
			slog.Info("thv-registry-api version",
				"version", info.Version,
				"commit", info.Commit,
				"built", info.BuildDate,
				"go", info.GoVersion,
				"platform", info.Platform)
		}
	},
}

func init() {
	versionCmd.Flags().String("format", "", "Output format (json)")
}
