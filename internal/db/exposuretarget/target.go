// Package exposuretarget holds the private, versioned offline endpoint fingerprint.
package exposuretarget

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// Digest retains the schema-29 consumed-target tuple unchanged at schema 30.
func Digest(ctx context.Context, tx pgx.Tx, databaseID string) (string, error) {
	var database string
	if err := tx.QueryRow(ctx, `SELECT current_database()`).Scan(&database); err != nil {
		return "", err
	}
	connection := tx.Conn().Config()
	encoded, err := json.Marshal([]any{"registry-exposure-consumed-target", 1, databaseID,
		connection.Host, connection.Port, database})
	if err != nil {
		return "", errors.New("cannot fingerprint consumed exposure target")
	}
	return fmt.Sprintf("sha256:%x", sha256.Sum256(encoded)), nil
}

// SetContext binds the endpoint and logical database identity to this connection.
func SetContext(ctx context.Context, tx pgx.Tx) error {
	var id string
	if err := tx.QueryRow(ctx, `SELECT database_id::text
 FROM public.exposure_acknowledgement WHERE singleton`).Scan(&id); err != nil {
		return err
	}
	digest, err := Digest(ctx, tx, id)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `SELECT set_config('toolhive.exposure_target_digest', $1, true)`, digest)
	return err
}

// Verify checks continuity without consuming, rebinding or changing evidence.
// The caller holds catalog/receipt locks and validates structural eligibility.
func Verify(ctx context.Context, tx pgx.Tx) error {
	var databaseID string
	var target *string
	if err := tx.QueryRow(ctx, `SELECT database_id::text, consumed_target_digest
 FROM public.exposure_acknowledgement WHERE singleton`).Scan(&databaseID, &target); err != nil {
		return errors.New("exposure target evidence unavailable")
	}
	if target == nil {
		return nil
	}
	actual, err := Digest(ctx, tx, databaseID)
	if err != nil {
		return errors.New("exposure database target unavailable")
	}
	if *target != actual {
		return errors.New("consumed exposure target mismatch; remain offline and use the reviewed endpoint")
	}
	return nil
}
