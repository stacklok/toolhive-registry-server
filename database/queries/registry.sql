-- name: InsertRegistry :exec
INSERT INTO registry (name, reg_type) VALUES ($1, $2);