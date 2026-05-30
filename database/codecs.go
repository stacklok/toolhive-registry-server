package database

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// enumArrayTypes are the custom PostgreSQL enum types whose array forms need a
// codec registered on every pgx connection. pgx cannot infer how to encode Go
// slices of these enums into PostgreSQL array types on its own.
var enumArrayTypes = []string{"sync_status", "icon_theme", "creation_type"}

// RegisterEnumArrayCodecs registers pgx array codecs for the schema's custom
// enum types. It is the AfterConnect hook used by both the production
// connection pool and the test database setup, so both encode enum-array
// parameters identically.
//
// The codecs are resolved from the live database — the enum OIDs are assigned
// at migration time — so this must run after migrations, on a connection that
// can read pg_type.
func RegisterEnumArrayCodecs(ctx context.Context, conn *pgx.Conn) error {
	for _, enumName := range enumArrayTypes {
		// Get the OID for the enum from the database.
		var enumOID uint32
		if err := conn.QueryRow(ctx, "SELECT oid FROM pg_type WHERE typname = $1", enumName).Scan(&enumOID); err != nil {
			return fmt.Errorf("failed to get %s OID: %w", enumName, err)
		}

		// Get the OID for the array type (PostgreSQL prefixes array types with _).
		var arrayOID uint32
		if err := conn.QueryRow(ctx, "SELECT oid FROM pg_type WHERE typname = $1", "_"+enumName).Scan(&arrayOID); err != nil {
			return fmt.Errorf("failed to get %s[] array OID: %w", enumName, err)
		}

		// Register the array codec with the proper element type codec.
		conn.TypeMap().RegisterType(&pgtype.Type{
			Name: enumName + "[]",
			OID:  arrayOID,
			Codec: &pgtype.ArrayCodec{
				ElementType: &pgtype.Type{
					Name:  enumName,
					OID:   enumOID,
					Codec: pgtype.TextCodec{},
				},
			},
		})
	}

	return nil
}
