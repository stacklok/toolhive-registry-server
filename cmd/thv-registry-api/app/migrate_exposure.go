package app

import (
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/spf13/cobra"

	"github.com/stacklok/toolhive-registry-server/internal/config"
	"github.com/stacklok/toolhive-registry-server/internal/db"
)

const (
	exposureInventoryCommand       = "inventory"
	exposureAcknowledgeCommand     = "acknowledge"
	exposureInitializeEmptyCommand = "initialize-empty"
)

func newMigrateExposureCmd() *cobra.Command {
	group := &cobra.Command{
		Use: "exposure", Short: "Offline catalog exposure review (legacy protections unchanged)",
		Long: `Inventory and record a dormant, revision-bound exposure acknowledgement.
Stop and drain all registry readers, writers and controllers before acknowledging.
These commands connect to an already migrated database; they never migrate,
initialize sources, repair latest pointers, fetch sources or start controllers.
An acknowledgement is NOT a cutover or proof that old processes are offline.
--yes does not imply exposure consent. Legacy claims/RBAC remain unchanged.`,
	}
	for _, mode := range []string{exposureInventoryCommand, exposureAcknowledgeCommand, exposureInitializeEmptyCommand} {
		command := &cobra.Command{Use: mode, Args: cobra.NoArgs, SilenceUsage: true}
		command.RunE = func(cmd *cobra.Command, _ []string) error { return runMigrateExposure(cmd, mode) }
		switch mode {
		case exposureInventoryCommand:
			command.Short = "Emit safe JSON inventory and review digest without writing"
		case exposureAcknowledgeCommand:
			command.Short = "Record whole-catalog approval; legacy protections unchanged"
			command.Flags().Bool("approve-whole-catalog", false, "Explicitly approve historical whole-catalog exposure")
		case exposureInitializeEmptyCommand:
			command.Short = "Record an explicit empty-install marker before reconciliation"
		}
		if mode != exposureInventoryCommand {
			command.Flags().String("digest", "", "Exact sha256: digest from the reviewed inventory")
			command.Flags().Bool("confirm-offline", false, "Confirm all readers, writers and controllers are stopped and drained")
			if err := command.MarkFlagRequired("digest"); err != nil {
				panic(err)
			}
		}
		group.AddCommand(command)
	}
	return group
}

func runMigrateExposure(cmd *cobra.Command, mode string) error {
	ctx := cmd.Context()
	digest := ""
	approve, offline := false, false
	if mode != exposureInventoryCommand {
		digest, _ = cmd.Flags().GetString("digest")
		offline, _ = cmd.Flags().GetBool("confirm-offline")
		if mode == exposureAcknowledgeCommand {
			approve, _ = cmd.Flags().GetBool("approve-whole-catalog")
		}
		if !offline || mode == exposureAcknowledgeCommand && !approve {
			return errors.New("explicit exposure consent and --confirm-offline are required; --yes is not consent")
		}
	}
	path, err := cmd.Flags().GetString("config")
	if err != nil {
		return errors.New("exposure config flag unavailable")
	}
	cfg, review, err := config.LoadExposureConfig(path)
	if err != nil {
		return err
	}
	connectionString, err := buildMigrationConnString(ctx, cfg.Database)
	if err != nil {
		return errors.New("exposure database connection configuration invalid")
	}
	conn, err := pgx.Connect(ctx, connectionString)
	if err != nil {
		return errors.New("exposure database connection failed")
	}
	defer func() { _ = conn.Close(ctx) }()
	var output any
	if mode == exposureInventoryCommand {
		inventory, err := db.InventoryExposure(ctx, conn, review.Revision)
		if err != nil {
			return err
		}
		output = struct {
			db.ExposureInventory
			Configuration config.ExposureConfig `json:"configuration"`
			Message       string                `json:"message"`
		}{inventory, review, "read-only inventory; legacy protections unchanged"}
	} else {
		ack, err := db.AcknowledgeExposure(ctx, conn, review.Revision, digest, approve, offline, mode == exposureInitializeEmptyCommand)
		if err != nil {
			return err
		}
		output = ack
	}
	if err := json.NewEncoder(cmd.OutOrStdout()).Encode(output); err != nil {
		return errors.New("cannot write exposure JSON output")
	}
	return nil
}
