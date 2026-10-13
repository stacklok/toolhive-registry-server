package db

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

var errExposureConsumeRetry = errors.New("concurrent exposure consumption")

// ConsumeExposure validates and consumes pending offline evidence, or verifies
// only target continuity on restart. The opt-in CatalogApp preparation barrier
// calls this after validating final configuration, freezing the loaded Cedar policy
// and finishing read-only source/operator compatibility preflight. The legacy CLI
// default does not select it. It never retires
// claims, initializes definitions, repairs storage or starts work.
func ConsumeExposure(ctx context.Context, conn *pgx.Conn, configRevision string) error {
	if !validExposureDigest(configRevision) {
		return errors.New("invalid exposure config revision")
	}
	for range 3 {
		err := consumeExposure(ctx, conn, configRevision)
		if !errors.Is(err, errExposureConsumeRetry) {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
	}
	return errors.New("concurrent exposure consumption; retry startup")
}

//nolint:gocyclo // Pending and consumed evidence have distinct validation rules within one atomic transition.
func consumeExposure(ctx context.Context, conn *pgx.Conn, revision string) error {
	tx, err := conn.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return consumeError(err, "cannot begin exposure consumption")
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	// Lock before acquiring a snapshot, including SELECT FOR UPDATE job owners.
	if _, err = tx.Exec(ctx, exposureCatalogLock); err != nil {
		return consumeError(err, "cannot lock exposure catalog; require clean schema version 29")
	}
	var version int
	var dirty bool
	err = tx.QueryRow(ctx, `SELECT version,dirty FROM public.schema_migrations`).Scan(&version, &dirty)
	if err != nil {
		return consumeError(err, "cannot inspect exposure schema")
	}
	if version != 29 || dirty {
		return errors.New("exposure consumption requires clean schema version 29; run migrate up separately")
	}
	var databaseID string
	var format int
	var id, mode, reviewed, postFence, configRevision, retirementID, postRetirement, consumedTarget *string
	var acknowledgedAt, retiredAt, consumedAt *time.Time
	err = tx.QueryRow(ctx, `SELECT database_id::text,inventory_version,
 acknowledgement_id::text,acknowledgement_mode,reviewed_digest,post_fence_digest,config_revision,acknowledged_at,
 retirement_id::text,post_retirement_digest,retired_at,consumed_at,consumed_target_digest
 FROM public.exposure_acknowledgement WHERE singleton`).Scan(&databaseID, &format,
		&id, &mode, &reviewed, &postFence, &configRevision, &acknowledgedAt,
		&retirementID, &postRetirement, &retiredAt, &consumedAt, &consumedTarget)
	if err != nil {
		return consumeError(err, "exposure consumption evidence unavailable")
	}
	if format != ExposureInventoryVersion {
		return errors.New("unsupported exposure inventory format")
	}
	target, err := consumedExposureTarget(ctx, tx, databaseID)
	if err != nil {
		return err
	}
	if consumedAt != nil {
		if consumedTarget == nil || *consumedTarget != target {
			return errors.New("consumed exposure target mismatch; use the reviewed endpoint " +
				"or restore an offline pre-retirement backup for new review")
		}
		// Historical digests/config/policy are immutable evidence, not a perpetual
		// startup revision: normal catalog and policy changes must remain possible.
		return consumeError(tx.Commit(ctx), "cannot finish exposure target verification")
	}
	if consumedTarget != nil || id == nil || mode == nil || reviewed == nil || postFence == nil ||
		configRevision == nil || acknowledgedAt == nil || !validExposureDigest(*reviewed) ||
		!validExposureDigest(*postFence) || *configRevision != revision {
		return errors.New("pending exposure acknowledgement missing or config/policy revision mismatch; remain offline")
	}
	digest := *postFence
	switch *mode {
	case "whole-catalog":
		if retirementID == nil || retiredAt == nil || postRetirement == nil || !validExposureDigest(*postRetirement) {
			return errors.New("existing catalog requires explicit offline claims retirement before consumption")
		}
		digest = *postRetirement
	case exposureEmptyMode:
		if retirementID != nil || retiredAt != nil || postRetirement != nil {
			return errors.New("invalid empty exposure evidence; remain offline")
		}
	default:
		return errors.New("unsupported exposure acknowledgement mode")
	}
	var live bool
	err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM public.registry_sync
 WHERE lease_id IS NOT NULL AND lease_expires_at > clock_timestamp())`).Scan(&live)
	if err != nil {
		return consumeError(err, "cannot inspect exposure leases")
	}
	if live {
		return errors.New("drain all unexpired sync leases, including stale generations; no lease was changed")
	}
	inventory, err := inventoryExposureTx(ctx, tx, revision)
	if err != nil {
		return err
	}
	if *mode == exposureEmptyMode {
		for _, count := range inventory.Counts {
			if count != 0 {
				return errors.New("empty exposure consumption requires every persisted catalog table to be empty")
			}
		}
	}
	if inventory.Digest != digest {
		return errors.New("pending exposure catalog or target digest mismatch; remain offline")
	}
	if _, err = tx.Exec(ctx, `UPDATE public.source SET definition_generation=definition_generation+1;
 UPDATE public.registry_sync SET applied_generation=NULL`); err != nil {
		return consumeError(err, "cannot fence exposure catalog")
	}
	if _, err = tx.Exec(ctx, `UPDATE public.exposure_acknowledgement
 SET consumed_target_digest=$1,consumed_at=clock_timestamp() WHERE singleton`, target); err != nil {
		return consumeError(err, "cannot record exposure consumption")
	}
	return consumeError(tx.Commit(ctx), "exposure consumption outcome unknown; remain offline; inspect stored receipt")
}

func consumedExposureTarget(ctx context.Context, tx pgx.Tx, databaseID string) (string, error) {
	var database string
	if err := tx.QueryRow(ctx, `SELECT current_database()`).Scan(&database); err != nil {
		return "", consumeError(err, "exposure database target unavailable")
	}
	connection := tx.Conn().Config()
	encoded, err := json.Marshal([]any{"registry-exposure-consumed-target", 1, databaseID,
		connection.Host, connection.Port, database})
	if err != nil {
		return "", errors.New("cannot fingerprint consumed exposure target")
	}
	return fmt.Sprintf("sha256:%x", sha256.Sum256(encoded)), nil
}

func consumeError(err error, message string) error {
	if err == nil {
		return nil
	}
	var pg *pgconn.PgError
	if errors.As(err, &pg) && (pg.Code == "40001" || pg.Code == "40P01") {
		return errExposureConsumeRetry
	}
	return errors.New(message)
}
