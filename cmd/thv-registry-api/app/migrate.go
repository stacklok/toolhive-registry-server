package app

import "github.com/spf13/cobra"

var migrateCmd = &cobra.Command{
	Use:   "migrate",
	Short: "Database migration tool",
	Long:  `Database migration tool for managing schema versions. Use with 'up' or 'down' subcommands.`,
	RunE: func(cmd *cobra.Command, _ []string) error {
		return cmd.Usage()
	},
}

func init() {
	migrateCmd.PersistentFlags().BoolP("yes", "y", false, "Confirm schema migrations only; never exposure consent")
	migrateCmd.PersistentFlags().UintP("num-steps", "n", 0, "Number of steps to migrate (0 = all)")
	migrateCmd.PersistentFlags().String("config", "", "Path to configuration file (YAML format, required)")

	if err := migrateCmd.MarkPersistentFlagRequired("config"); err != nil {
		panic(err)
	}

	// Add subcommands
	migrateUpCmd.Flags().Uint("to-version", 0, "Explicit forward schema target (29 for offline exposure review; default latest)")
	migrateCmd.AddCommand(migrateUpCmd)
	migrateCmd.AddCommand(migrateDownCmd)
	migrateCmd.AddCommand(newMigrateExposureCmd())
}
