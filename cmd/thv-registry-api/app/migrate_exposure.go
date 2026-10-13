package app

import (
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/spf13/cobra"

	registryapp "github.com/stacklok/toolhive-registry-server/internal/app"
	"github.com/stacklok/toolhive-registry-server/internal/config"
	"github.com/stacklok/toolhive-registry-server/internal/db"
)

const (
	exposureInventoryCommand       = "inventory"
	exposureAcknowledgeCommand     = "acknowledge"
	exposureInitializeEmptyCommand = "initialize-empty"
	exposureRetireClaimsCommand    = "retire-claims"
)

func newMigrateExposureCmd() *cobra.Command {
	group := &cobra.Command{
		Use: "exposure", Short: "Offline catalog exposure review and explicit metadata retirement",
		Long: `Inventory and record a dormant, revision-bound exposure acknowledgement.
Stop and drain all registry readers, writers and controllers before acknowledging.
These commands connect to an already migrated database; they never migrate,
initialize sources, repair latest pointers, fetch sources or start controllers.
An acknowledgement is NOT a cutover or proof that old processes are offline.
Only retire-claims archives and clears historical claims. Finalize consumes the
reviewed receipt after final config, frozen Cedar and full compatibility preflight.
Remain OFFLINE until finalize and forward schema cleanup both succeed.
--yes confirms schema migration only; it does not imply exposure consent.`,
	}
	for _, mode := range []string{
		exposureInventoryCommand, exposureAcknowledgeCommand, exposureInitializeEmptyCommand, exposureRetireClaimsCommand,
	} {
		command := &cobra.Command{Use: mode, Args: cobra.NoArgs, SilenceUsage: true}
		command.RunE = func(cmd *cobra.Command, _ []string) error { return runMigrateExposure(cmd, mode) }
		switch mode {
		case exposureInventoryCommand:
			command.Short = "Emit safe JSON inventory and review digest without writing"
		case exposureAcknowledgeCommand:
			command.Short = "Record whole-catalog approval; legacy protections unchanged"
			command.Flags().Bool("approve-whole-catalog", false, "Explicitly approve historical whole-catalog exposure")
		case exposureRetireClaimsCommand:
			command.Short = "Archive and clear claims OFFLINE before finalize and schema cleanup"
			command.Long = `Dangerous offline-only metadata transition, not runtime activation.
Prepare the FINAL config and policy without retired fields BEFORE inventory and
acknowledgement. Match that pending acknowledgement ID and its post-fence digest.
Back up and stop/drain ALL readers, writers and controllers. Archive ALL source,
view and entry claims, clear them to SQL NULL and fence old jobs atomically.
Remain OFFLINE afterward: finalize the existing receipt, then migrate up to 30.
Never restart a legacy runtime against retired metadata.
Repeats are rejected without changes; a lost response or receipt output failure
requires remaining offline and inspecting stored retirement state as documented in
docs/registry-exposure-migration.md, not re-acknowledging or rebinding evidence.`
			command.Flags().String("acknowledgement-id", "", "Exact pending whole-catalog acknowledgement UUID")
			command.Flags().Bool("approve-whole-catalog", false, "Explicitly approve archive-and-clear of all historical claims")
			if err := command.MarkFlagRequired("acknowledgement-id"); err != nil {
				panic(err)
			}
		case exposureInitializeEmptyCommand:
			command.Short = "Record an explicit empty-install marker before reconciliation"
		}
		if mode != exposureInventoryCommand {
			command.Flags().String("digest", "", "Exact sha256: reviewed inventory digest (post-fence digest for retire-claims)")
			command.Flags().Bool("confirm-offline", false, "Confirm all readers, writers and controllers are stopped and drained")
			if err := command.MarkFlagRequired("digest"); err != nil {
				panic(err)
			}
		}
		group.AddCommand(command)
	}
	finalize := &cobra.Command{
		Use: "finalize", Short: "Consume or verify offline exposure evidence before schema cleanup",
		Args: cobra.NoArgs, SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			offline, _ := cmd.Flags().GetBool("confirm-offline")
			if !offline {
				return errors.New("finalize requires --confirm-offline; --yes is not consent")
			}
			path, _ := cmd.Flags().GetString("config")
			cfg, err := config.LoadCatalogConfig(config.WithConfigPath(path))
			if err != nil {
				return errors.New("final exposure configuration unavailable or invalid")
			}
			if err := registryapp.FinalizeExposure(cmd.Context(), registryapp.CatalogOptions{Config: cfg}); err != nil {
				return err
			}
			if err := json.NewEncoder(cmd.OutOrStdout()).Encode(struct {
				Message string `json:"message"`
			}{
				"exposure finalized; remain offline until schema cleanup completes",
			}); err != nil {
				return errors.New("exposure finalization committed or verified; remain offline; inspect stored receipt")
			}
			return nil
		},
	}
	finalize.Flags().Bool("confirm-offline", false, "Confirm all readers, writers and controllers are stopped and drained")
	group.AddCommand(finalize)
	return group
}

//nolint:gocyclo // Keep consent checks and safe connection/output handling together for the four offline commands.
func runMigrateExposure(cmd *cobra.Command, mode string) error {
	ctx := cmd.Context()
	digest := ""
	approve, offline := false, false
	if mode != exposureInventoryCommand {
		digest, _ = cmd.Flags().GetString("digest")
		offline, _ = cmd.Flags().GetBool("confirm-offline")
		if mode == exposureAcknowledgeCommand || mode == exposureRetireClaimsCommand {
			approve, _ = cmd.Flags().GetBool("approve-whole-catalog")
		}
		if !offline || (mode == exposureAcknowledgeCommand || mode == exposureRetireClaimsCommand) && !approve {
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
	if mode == exposureRetireClaimsCommand && len(review.RetiredFields) != 0 {
		return errors.New("retire-claims requires final configuration without retired fields; " +
			"prepare it before inventory and acknowledgement")
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
	switch mode {
	case exposureInventoryCommand:
		inventory, err := db.InventoryExposure(ctx, conn, review.Revision)
		if err != nil {
			return err
		}
		output = struct {
			db.ExposureInventory
			Configuration config.ExposureConfig `json:"configuration"`
			Message       string                `json:"message"`
		}{inventory, review, "read-only inventory; legacy protections unchanged"}
	case exposureRetireClaimsCommand:
		id, _ := cmd.Flags().GetString("acknowledgement-id")
		receipt, err := db.RetireExposureClaims(ctx, conn, review.Revision, id, digest, approve, offline)
		if err != nil {
			return err
		}
		output = receipt
	default:
		ack, err := db.AcknowledgeExposure(ctx, conn, review.Revision, digest, approve, offline, mode == exposureInitializeEmptyCommand)
		if err != nil {
			return err
		}
		output = ack
	}
	if err := json.NewEncoder(cmd.OutOrStdout()).Encode(output); err != nil {
		if mode == exposureRetireClaimsCommand {
			return errors.New(
				"claim metadata retirement committed; remain offline; inspect stored receipt in docs/registry-exposure-migration.md")
		}
		return errors.New("cannot write exposure JSON output")
	}
	return nil
}
