package postgres

import (
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
)

// classifySnapshotWrite classifies a snapshot write failure. A failure caused
// by the snapshot's content (SQLSTATE class 22, data exception, such as an
// invalid enum value or byte sequence; or 54000, such as an index row too
// large) is persistence.ErrInvalid: it belongs to the source, which keeps its
// previous data, and must not read as an unhealthy backend. The message keeps
// the database's reason for the sync failure log. Everything else is classify.
func classifySnapshotWrite(err error) error {
	var pg *pgconn.PgError
	if errors.As(err, &pg) && (strings.HasPrefix(pg.Code, "22") || pg.Code == "54000") {
		return fmt.Errorf("%w: the database rejected the snapshot content (SQLSTATE %s): %s",
			persistence.ErrInvalid, pg.Code, pg.Message)
	}
	return classify(err)
}
