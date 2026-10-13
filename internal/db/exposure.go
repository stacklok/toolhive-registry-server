package db

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

// ExposureInventoryVersion pins the explicit catalog coverage and encoding.
const ExposureInventoryVersion = 1

const exposureSourceTable = "source"
const exposureVersionID = "version_id"
const exposureEmptyMode = "empty"

const exposureCatalogLock = `LOCK TABLE public.source, public.registry_sync, public.registry,
 public.registry_source, public.registry_entry, public.entry_version,
 public.mcp_server, public.mcp_server_package, public.mcp_server_remote, public.mcp_server_icon,
 public.skill, public.skill_oci_package, public.skill_git_package,
 public.plugin, public.plugin_oci_package, public.plugin_git_package, public.latest_entry_version,
 public.exposure_acknowledgement, public.schema_migrations IN EXCLUSIVE MODE`

// ExposureRecord is safe review metadata. Claims and payload values never escape.
type ExposureRecord struct {
	Table      string `json:"table"`
	ID         string `json:"id"`
	Name       string `json:"name"`
	SourceID   string `json:"source_id,omitempty"`
	Kind       string `json:"kind,omitempty"`
	Ownership  string `json:"ownership,omitempty"`
	Generation int64  `json:"generation,omitempty"`
	Claims     string `json:"claims"`
	Unlinked   bool   `json:"unlinked"`
}

// ExposureLink preserves the review-visible ordering of named views.
type ExposureLink struct {
	RegistryID string `json:"registry_id"`
	SourceID   string `json:"source_id"`
	Position   int    `json:"position"`
}

// ExposureInventory describes all persisted catalog state, including retained
// snapshots, without exposing secrets, policies, payloads or raw claims.
type ExposureInventory struct {
	Version        int              `json:"version"`
	DatabaseID     string           `json:"database_id"`
	ConfigRevision string           `json:"config_revision"`
	Digest         string           `json:"digest"`
	Counts         map[string]int64 `json:"counts"`
	Records        []ExposureRecord `json:"records"`
	Links          []ExposureLink   `json:"links"`
}

// ExposureAcknowledgement is pending review state, not a runtime permission.
type ExposureAcknowledgement struct {
	ID              string `json:"acknowledgement_id"`
	Mode            string `json:"mode"`
	ReviewedDigest  string `json:"reviewed_digest"`
	PostFenceDigest string `json:"post_fence_digest"`
	Message         string `json:"message"`
}

// InventoryExposure uses a consistent read-only snapshot and never migrates,
// initializes definitions or repairs latest pointers.
func InventoryExposure(ctx context.Context, conn *pgx.Conn, configRevision string) (ExposureInventory, error) {
	if !validExposureDigest(configRevision) {
		return ExposureInventory{}, errors.New("invalid exposure config revision")
	}
	tx, err := conn.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return ExposureInventory{}, errors.New("cannot begin exposure inventory")
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	inventory, err := inventoryExposureTx(ctx, tx, configRevision)
	if err != nil {
		return ExposureInventory{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return ExposureInventory{}, errors.New("cannot finish exposure inventory")
	}
	return inventory, nil
}

// AcknowledgeExposure records a revision-bound dormant marker. The caller must
// stop/drain every old process; neither a digest nor a lease check proves that.
//
//nolint:gocyclo // Consent, digest, lease and rollback checks belong to one atomic offline operation.
func AcknowledgeExposure(ctx context.Context, conn *pgx.Conn, configRevision, digest string,
	approveWholeCatalog, confirmOffline, initializeEmpty bool,
) (ExposureAcknowledgement, error) {
	if !confirmOffline || !initializeEmpty && !approveWholeCatalog || initializeEmpty && approveWholeCatalog {
		return ExposureAcknowledgement{}, errors.New("explicit exposure mode and --confirm-offline are required")
	}
	if !validExposureDigest(digest) || !validExposureDigest(configRevision) {
		return ExposureAcknowledgement{}, errors.New("invalid exposure digest or config revision")
	}
	tx, err := conn.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return ExposureAcknowledgement{}, errors.New("cannot begin exposure acknowledgement")
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	// Lock before the first snapshot read. EXCLUSIVE also excludes SELECT FOR
	// UPDATE job fences; ordinary readers can continue. No runtime is started.
	if _, err = tx.Exec(ctx, exposureCatalogLock); err != nil {
		return ExposureAcknowledgement{}, errors.New(
			"cannot lock exposure catalog; require clean schema version 27, 28 or 29 and offline drain")
	}
	inventory, err := inventoryExposureTx(ctx, tx, configRevision)
	if err != nil {
		return ExposureAcknowledgement{}, err
	}
	var schemaVersion int
	if err = tx.QueryRow(ctx, `SELECT version FROM public.schema_migrations`).Scan(&schemaVersion); err != nil {
		return ExposureAcknowledgement{}, errors.New("cannot inspect exposure schema")
	}
	if schemaVersion >= 28 {
		var transitioned bool
		if err = tx.QueryRow(ctx, `SELECT retirement_id IS NOT NULL OR consumed_at IS NOT NULL
 FROM public.exposure_acknowledgement WHERE singleton`).Scan(&transitioned); err != nil {
			return ExposureAcknowledgement{}, errors.New("cannot inspect exposure transition")
		}
		if transitioned {
			return ExposureAcknowledgement{}, errors.New("exposure already transitioned; acknowledgement evidence cannot be replaced")
		}
		var inventoryVersion int
		if err = tx.QueryRow(ctx, `SELECT inventory_version FROM public.exposure_acknowledgement WHERE singleton`).Scan(
			&inventoryVersion); err != nil {
			return ExposureAcknowledgement{}, errors.New("cannot inspect exposure format")
		}
		if schemaVersion == 30 && (inventoryVersion != 2 || !initializeEmpty) {
			return ExposureAcknowledgement{}, errors.New("historical review cannot be rebound at schema 30; finalize offline at schema 29")
		}
		if initializeEmpty {
			var archived bool
			if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM public.exposure_claims_archive)`).Scan(&archived); err != nil {
				return ExposureAcknowledgement{}, errors.New("cannot inspect exposure archive")
			}
			if archived {
				return ExposureAcknowledgement{}, errors.New("initialize-empty cannot reuse archived catalog history")
			}
		}
	}
	if inventory.Digest != digest {
		return ExposureAcknowledgement{}, errors.New("exposure digest mismatch; inventory and review again")
	}
	if initializeEmpty {
		for _, count := range inventory.Counts {
			if count != 0 {
				return ExposureAcknowledgement{}, errors.New("initialize-empty requires every persisted catalog table to be empty")
			}
		}
	}
	var live bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM public.registry_sync
 WHERE lease_id IS NOT NULL AND lease_expires_at > clock_timestamp())`).Scan(&live); err != nil {
		return ExposureAcknowledgement{}, errors.New("cannot inspect exposure leases")
	}
	if live {
		return ExposureAcknowledgement{}, errors.New(
			"drain all unexpired sync leases, including stale generations; no lease was changed")
	}
	if _, err = tx.Exec(ctx, `UPDATE public.source SET definition_generation=definition_generation+1;
 UPDATE public.registry_sync SET applied_generation=NULL`); err != nil {
		return ExposureAcknowledgement{}, errors.New("cannot fence exposure catalog")
	}
	postFence, err := inventoryExposureTx(ctx, tx, configRevision)
	if err != nil {
		return ExposureAcknowledgement{}, err
	}
	mode := "whole-catalog"
	if initializeEmpty {
		mode = exposureEmptyMode
	}
	ack := ExposureAcknowledgement{Mode: mode, ReviewedDigest: digest, PostFenceDigest: postFence.Digest,
		Message: "acknowledgement recorded; legacy protections unchanged"}
	if err = tx.QueryRow(ctx, `UPDATE public.exposure_acknowledgement SET
 acknowledgement_id=gen_random_uuid(), acknowledgement_mode=$1, reviewed_digest=$2,
 post_fence_digest=$3, config_revision=$4, acknowledged_at=clock_timestamp()
 WHERE singleton RETURNING acknowledgement_id::text`, mode, digest, postFence.Digest, configRevision).Scan(&ack.ID); err != nil {
		return ExposureAcknowledgement{}, errors.New("cannot record exposure acknowledgement")
	}
	if err = tx.Commit(ctx); err != nil {
		return ExposureAcknowledgement{}, errors.New("exposure acknowledgement rolled back; inventory and review again")
	}
	return ack, nil
}

// This bounded list is intentionally explicit, not a schema-discovery framework.
// Every column is fingerprinted; ordering is by a complete unique key. JSONB
// columns also carry SQL NULL flags because row_to_json collapses SQL/JSON null.
var exposureTables = []struct {
	name, order string
	claims      bool
	jsonNulls   string
}{
	{exposureSourceTable, "id", true, "source_config IS NULL, filter_config IS NULL"},
	{"registry", "id", true, ""},
	{"registry_source", "registry_id, position, source_id", false, ""},
	{"registry_entry", "id", true, ""},
	{"entry_version", "id", false, ""},
	{"mcp_server", exposureVersionID, false, "upstream_meta IS NULL, server_meta IS NULL"},
	{"mcp_server_package", `server_id, registry_type COLLATE "C", pkg_identifier COLLATE "C", transport COLLATE "C"`, false,
		"runtime_arguments IS NULL, package_arguments IS NULL, env_vars IS NULL, " +
			"transport_headers IS NULL, transport_variables IS NULL"},
	{"mcp_server_remote", `server_id, transport COLLATE "C", transport_url COLLATE "C"`, false,
		"transport_headers IS NULL, transport_variables IS NULL"},
	{"mcp_server_icon", `server_id, source_uri COLLATE "C", mime_type COLLATE "C", theme`, false, ""},
	{"skill", exposureVersionID, false,
		"repository IS NULL, icons IS NULL, metadata IS NULL, extension_meta IS NULL, provenance IS NULL"},
	{"skill_oci_package", "id", false, ""},
	{"skill_git_package", "id", false, ""},
	{"plugin", exposureVersionID, false, "repository IS NULL, icons IS NULL, metadata IS NULL, extension_meta IS NULL"},
	{"plugin_oci_package", "id", false, ""},
	{"plugin_git_package", "id", false, ""},
	{"latest_entry_version", `source_id, entry_type, name COLLATE "C"`, false, ""},
	{"registry_sync", "id", false, ""},
}

//nolint:gocyclo // Explicit bounded table streaming keeps row decoding and its error paths together.
func inventoryExposureTx(ctx context.Context, tx pgx.Tx, revision string) (ExposureInventory, error) {
	if _, err := tx.Exec(ctx, `SET LOCAL TIME ZONE 'UTC'; SET LOCAL intervalstyle = 'iso_8601'`); err != nil {
		return ExposureInventory{}, errors.New("cannot configure exposure inventory")
	}
	var version int
	var dirty bool
	err := tx.QueryRow(ctx, `SELECT version,dirty FROM public.schema_migrations`).Scan(&version, &dirty)
	if err != nil || (version != 27 && version != 28 && version != 29 && version != 30) || dirty {
		return ExposureInventory{}, errors.New(
			"exposure inventory requires clean schema version 27, 28 or 29 (or claims-free 30); run migrate up separately")
	}
	inventory := ExposureInventory{Version: ExposureInventoryVersion, ConfigRevision: revision,
		Counts: map[string]int64{}, Records: []ExposureRecord{}, Links: []ExposureLink{}}
	if version == 30 {
		inventory.Version = 2
	}
	var format int
	if err := tx.QueryRow(ctx, `SELECT database_id::text,inventory_version
 FROM public.exposure_acknowledgement WHERE singleton`).Scan(
		&inventory.DatabaseID, &format); err != nil || (format != 1 && format != inventory.Version) {
		return ExposureInventory{}, errors.New("exposure database identity unavailable")
	}
	var database string
	if err := tx.QueryRow(ctx, `SELECT current_database()`).Scan(&database); err != nil {
		return ExposureInventory{}, errors.New("exposure database target unavailable")
	}
	h := sha256.New()
	encoder := json.NewEncoder(h)
	connection := tx.Conn().Config()
	if err := encoder.Encode([]any{"registry-exposure", inventory.Version, inventory.DatabaseID,
		connection.Host, connection.Port, database, revision}); err != nil {
		return ExposureInventory{}, errors.New("cannot fingerprint exposure target")
	}
	linked := map[string]bool{}
	for _, table := range exposureTables {
		projection := "'row',row_to_json(t)"
		if table.claims && version < 30 {
			projection += " ,'claims_sql_null',claims IS NULL"
		}
		if table.jsonNulls != "" {
			projection += ",'sql_nulls',ARRAY[" + table.jsonNulls + "]"
		}
		query := "SELECT json_build_object(" + projection + ")::text FROM public." + table.name + " t ORDER BY " + table.order
		rows, err := tx.Query(ctx, query)
		if err != nil {
			return ExposureInventory{}, errors.New("cannot read exposure catalog table " + table.name)
		}
		inventory.Counts[table.name] = 0
		for rows.Next() {
			var data []byte
			if err = rows.Scan(&data); err != nil {
				break
			}
			var value any
			decoder := json.NewDecoder(bytes.NewReader(data))
			decoder.UseNumber()
			if err = decoder.Decode(&value); err != nil {
				break
			}
			if err = encoder.Encode([]any{table.name, value}); err != nil {
				break
			}
			inventory.Counts[table.name]++
			if table.claims {
				var envelope struct {
					Row     exposureRow
					SQLNull bool `json:"claims_sql_null"`
				}
				if err = json.Unmarshal(data, &envelope); err != nil {
					break
				}
				r := envelope.Row
				if table.name == exposureSourceTable {
					r.Kind = r.SourceType
				}
				class := "removed"
				if version < 30 {
					class = exposureClaimsClass(r.Claims, envelope.SQLNull)
				}
				inventory.Records = append(inventory.Records, ExposureRecord{Table: table.name, ID: r.ID, Name: r.Name,
					SourceID: r.SourceID, Kind: r.Kind, Ownership: r.Ownership, Generation: r.Generation,
					Claims: class})
			}
			if table.name == "registry_source" {
				var link struct{ Row ExposureLink }
				if err = json.Unmarshal(data, &link); err != nil {
					break
				}
				inventory.Links = append(inventory.Links, link.Row)
				linked[link.Row.SourceID] = true
			}
		}
		rows.Close()
		if err != nil || rows.Err() != nil {
			return ExposureInventory{}, errors.New("cannot fingerprint exposure catalog table " + table.name)
		}
		if err := encoder.Encode([]any{table.name, inventory.Counts[table.name]}); err != nil {
			return ExposureInventory{}, errors.New("cannot fingerprint exposure catalog counts")
		}
	}
	for i := range inventory.Records {
		r := &inventory.Records[i]
		switch r.Table {
		case exposureSourceTable:
			r.Unlinked = !linked[r.ID]
		case "registry_entry":
			r.Unlinked = !linked[r.SourceID]
		}
	}
	inventory.Digest = fmt.Sprintf("sha256:%x", h.Sum(nil))
	return inventory, nil
}

type exposureRow struct {
	ID         string          `json:"id"`
	Name       string          `json:"name"`
	SourceID   string          `json:"source_id"`
	Kind       string          `json:"entry_type"`
	SourceType string          `json:"source_type"`
	Ownership  string          `json:"creation_type"`
	Generation int64           `json:"definition_generation"`
	Claims     json.RawMessage `json:"claims"`
}

func exposureClaimsClass(data []byte, sqlNull bool) string {
	if sqlNull {
		return "sql-null"
	}
	if string(data) == "null" {
		return "json-null"
	}
	var claims map[string]any
	if err := json.Unmarshal(data, &claims); err != nil || claims == nil {
		return "malformed"
	}
	if len(claims) == 0 {
		return "empty-object"
	}
	if err := ValidateClaimValues(claims); err != nil {
		return "malformed"
	}
	return "labeled"
}

func validExposureDigest(value string) bool {
	if !strings.HasPrefix(value, "sha256:") || len(value) != 71 {
		return false
	}
	_, err := hex.DecodeString(strings.TrimPrefix(value, "sha256:"))
	return err == nil && strings.ToLower(value) == value
}
