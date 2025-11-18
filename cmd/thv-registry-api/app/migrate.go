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
	migrateCmd.PersistentFlags().BoolP("yes", "y", false, "Answer yes to all questions")
	migrateCmd.PersistentFlags().UintP("num-steps", "n", 0, "Number of steps to migrate (0 = all)")
	migrateCmd.PersistentFlags().String("config", "", "Path to configuration file (YAML format, required)")

	if err := migrateCmd.MarkPersistentFlagRequired("config"); err != nil {
		panic(err)
	}

	// Add subcommands
	migrateCmd.AddCommand(migrateUpCmd)
	migrateCmd.AddCommand(migrateDownCmd)
}
