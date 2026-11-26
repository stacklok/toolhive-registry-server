-- name: RecordAuthorizationMode :exec
-- Records the mode on a database that has none yet; a recorded mode is kept.
INSERT INTO authorization_mode (mode) VALUES (sqlc.arg(mode))
ON CONFLICT (singleton) DO NOTHING;

-- name: GetAuthorizationMode :one
SELECT mode FROM authorization_mode;
