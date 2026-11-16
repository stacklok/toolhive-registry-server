package db

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

// ExposureRetirement is a dormant receipt, not permission to start a runtime.
type ExposureRetirement struct {
	AcknowledgementID    string `json:"acknowledgement_id"`
	RetirementID         string `json:"retirement_id"`
	ReviewedDigest       string `json:"reviewed_digest"`
	PostFenceDigest      string `json:"post_fence_digest"`
	PostRetirementDigest string `json:"post_retirement_digest"`
	Message              string `json:"message"`
}

// RetireExposureClaims archives all historical claim classifications and clears
// only claims, with new job fences. No serving path consumes this receipt yet.
//
//nolint:gocyclo // Consent, evidence, lease and rollback checks form one offline transition.
func RetireExposureClaims(ctx context.Context, conn *pgx.Conn, configRevision, acknowledgementID, digest string,
	approveWholeCatalog, confirmOffline bool,
) (ExposureRetirement, error) {
	if !approveWholeCatalog || !confirmOffline {
		return ExposureRetirement{}, errors.New("--approve-whole-catalog and --confirm-offline are required")
	}
	if acknowledgementID == "" || !validExposureDigest(digest) || !validExposureDigest(configRevision) {
		return ExposureRetirement{}, errors.New("valid acknowledgement ID, digest and config revision are required")
	}
	tx, err := conn.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return ExposureRetirement{}, errors.New("cannot begin exposure retirement")
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	if _, err = tx.Exec(ctx, exposureCatalogLock); err != nil {
		return ExposureRetirement{}, errors.New("cannot lock exposure catalog; require clean schema version 28 or 29 and offline drain")
	}
	var version int
	var dirty bool
	err = tx.QueryRow(ctx, `SELECT version,dirty FROM public.schema_migrations`).Scan(&version, &dirty)
	if err != nil || version != 28 && version != 29 || dirty {
		return ExposureRetirement{}, errors.New("exposure retirement requires clean schema version 28 or 29; run migrate up separately")
	}
	if _, err = tx.Exec(ctx, `LOCK TABLE public.exposure_claims_archive IN EXCLUSIVE MODE`); err != nil {
		return ExposureRetirement{}, errors.New("cannot lock exposure archive")
	}
	receipt := ExposureRetirement{AcknowledgementID: acknowledgementID,
		Message: "metadata retired; runtime cutover not yet available in this slice"}
	var matches bool
	if err = tx.QueryRow(ctx, `SELECT
 acknowledgement_id::text=$1 AND acknowledgement_mode='whole-catalog'
 AND config_revision=$2 AND post_fence_digest=$3
 AND retirement_id IS NULL AND consumed_at IS NULL,
 COALESCE(reviewed_digest,''),COALESCE(post_fence_digest,'')
 FROM public.exposure_acknowledgement WHERE singleton`, acknowledgementID, configRevision, digest).Scan(
		&matches, &receipt.ReviewedDigest, &receipt.PostFenceDigest); err != nil || !matches {
		return ExposureRetirement{}, errors.New(
			"pending whole-catalog acknowledgement mismatch or already transitioned; no metadata changed")
	}
	var live bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM public.registry_sync
 WHERE lease_id IS NOT NULL AND lease_expires_at > clock_timestamp())`).Scan(&live); err != nil {
		return ExposureRetirement{}, errors.New("cannot inspect exposure leases")
	}
	if live {
		return ExposureRetirement{}, errors.New("drain all unexpired sync leases, including stale generations; no lease was changed")
	}
	inventory, err := inventoryExposureTx(ctx, tx, configRevision)
	if err != nil {
		return ExposureRetirement{}, err
	}
	if inventory.Digest != digest {
		return ExposureRetirement{}, errors.New("exposure digest mismatch; inventory and review again")
	}
	if err = tx.QueryRow(ctx, `SELECT gen_random_uuid()::text`).Scan(&receipt.RetirementID); err != nil {
		return ExposureRetirement{}, errors.New("cannot create exposure retirement identity")
	}
	if _, err = tx.Exec(ctx, `INSERT INTO public.exposure_claims_archive
 (retirement_id,row_table,row_id,name,source_id,source_name,kind,ownership,claims_sql_null,claims)
 SELECT $1::uuid,'source',id,name,id,name,source_type,creation_type::text,claims IS NULL,claims FROM public.source
 UNION ALL
 SELECT $1,'registry',id,name,NULL,NULL,NULL,creation_type::text,claims IS NULL,claims FROM public.registry
 UNION ALL
 SELECT $1,'registry_entry',e.id,e.name,e.source_id,s.name,e.entry_type::text,s.creation_type::text,e.claims IS NULL,e.claims
 FROM public.registry_entry e JOIN public.source s ON s.id=e.source_id`, receipt.RetirementID); err != nil {
		return ExposureRetirement{}, errors.New("cannot archive exposure claims")
	}
	if _, err = tx.Exec(ctx, `UPDATE public.source SET claims=NULL,definition_generation=definition_generation+1;
 UPDATE public.registry SET claims=NULL;
 UPDATE public.registry_entry SET claims=NULL;
 UPDATE public.registry_sync SET applied_generation=NULL`); err != nil {
		return ExposureRetirement{}, errors.New("cannot clear and fence exposure metadata")
	}
	post, err := inventoryExposureTx(ctx, tx, configRevision)
	if err != nil {
		return ExposureRetirement{}, err
	}
	receipt.PostRetirementDigest = post.Digest
	if _, err = tx.Exec(ctx, `UPDATE public.exposure_acknowledgement SET
 retirement_id=$1,post_retirement_digest=$2,retired_at=clock_timestamp() WHERE singleton`,
		receipt.RetirementID, receipt.PostRetirementDigest); err != nil {
		return ExposureRetirement{}, errors.New("cannot record exposure retirement")
	}
	if err = tx.Commit(ctx); err != nil {
		return ExposureRetirement{}, errors.New(
			"claim metadata retirement outcome unknown; remain offline; inspect stored receipt in docs/registry-exposure-migration.md")
	}
	return receipt, nil
}
