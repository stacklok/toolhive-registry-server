package config

import (
	"context"
	"errors"
)

// LoadSecret reads a secret file for source ingestion, such as a Git
// password, using the same absolute-path and symlink rules as config loading.
// Its error never includes the path or the file's contents.
func LoadSecret(ctx context.Context, path string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	value, err := readSecretFromFile(path)
	if err != nil {
		return "", errors.New("secret reference unavailable")
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return value, nil
}
