package database

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/aws/smithy-go/ptr"
	"github.com/google/uuid"
	upstreamv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	model "github.com/modelcontextprotocol/registry/pkg/model"

	"github.com/stacklok/toolhive-registry-server/internal/db/sqlc"
)

// helper is just a bridge between the database and the API schema,
// useful to avoid writing the same conversion routine twice.
//
// Ideally, this would not be necessary, and sqlc would be able to
// detect that the two underlying statements have the same columns,
// but for some reason it does not and we have to duplicate at this
// layer.
type helper struct {
	RegistryType        sqlc.RegistryType
	ID                  uuid.UUID
	Name                string
	Version             string
	IsLatest            bool
	CreatedAt           *time.Time
	UpdatedAt           *time.Time
	Description         *string
	Title               *string
	Website             *string
	UpstreamMeta        []byte
	ServerMeta          []byte
	RepositoryUrl       *string
	RepositoryID        *string
	RepositorySubfolder *string
	RepositoryType      *string
}

func listServersRowToHelper(
	dbServer sqlc.ListServersRow,
) helper {
	return helper{
		RegistryType:        dbServer.RegistryType,
		ID:                  dbServer.ID,
		Name:                dbServer.Name,
		Version:             dbServer.Version,
		IsLatest:            dbServer.IsLatest,
		CreatedAt:           dbServer.CreatedAt,
		UpdatedAt:           dbServer.UpdatedAt,
		Description:         dbServer.Description,
		Title:               dbServer.Title,
		Website:             dbServer.Website,
		UpstreamMeta:        dbServer.UpstreamMeta,
		ServerMeta:          dbServer.ServerMeta,
		RepositoryUrl:       dbServer.RepositoryUrl,
		RepositoryID:        dbServer.RepositoryID,
		RepositorySubfolder: dbServer.RepositorySubfolder,
		RepositoryType:      dbServer.RepositoryType,
	}
}

func listServerVersionsRowToHelper(
	dbServer sqlc.ListServerVersionsRow,
) helper {
	return helper{
		RegistryType:        dbServer.RegistryType,
		ID:                  dbServer.ID,
		Name:                dbServer.Name,
		Version:             dbServer.Version,
		IsLatest:            dbServer.IsLatest,
		CreatedAt:           dbServer.CreatedAt,
		UpdatedAt:           dbServer.UpdatedAt,
		Description:         dbServer.Description,
		Title:               dbServer.Title,
		Website:             dbServer.Website,
		UpstreamMeta:        dbServer.UpstreamMeta,
		ServerMeta:          dbServer.ServerMeta,
		RepositoryUrl:       dbServer.RepositoryUrl,
		RepositoryID:        dbServer.RepositoryID,
		RepositorySubfolder: dbServer.RepositorySubfolder,
		RepositoryType:      dbServer.RepositoryType,
	}
}

func getServerVersionRowToHelper(
	dbServer sqlc.GetServerVersionRow,
) helper {
	return helper{
		RegistryType:        dbServer.RegistryType,
		ID:                  dbServer.ID,
		Name:                dbServer.Name,
		Version:             dbServer.Version,
		IsLatest:            dbServer.IsLatest,
		CreatedAt:           dbServer.CreatedAt,
		UpdatedAt:           dbServer.UpdatedAt,
		Description:         dbServer.Description,
		Title:               dbServer.Title,
		Website:             dbServer.Website,
		UpstreamMeta:        dbServer.UpstreamMeta,
		ServerMeta:          dbServer.ServerMeta,
		RepositoryUrl:       dbServer.RepositoryUrl,
		RepositoryID:        dbServer.RepositoryID,
		RepositorySubfolder: dbServer.RepositorySubfolder,
		RepositoryType:      dbServer.RepositoryType,
	}
}

func helperToServer(
	dbServer helper,
	packages []sqlc.ListServerPackagesRow,
	remotes []sqlc.McpServerRemote,
) upstreamv0.ServerJSON {
	server := upstreamv0.ServerJSON{
		Schema:      "https://static.modelcontextprotocol.io/schemas/2025-12-11/server.schema.json",
		Name:        dbServer.Name,
		Description: ptr.ToString(dbServer.Description),
		Title:       ptr.ToString(dbServer.Title),
		Version:     dbServer.Version,
		WebsiteURL:  ptr.ToString(dbServer.Website),
		Packages:    toPackages(packages),
		Remotes:     toRemotes(remotes),
	}

	if dbServer.RepositoryUrl != nil {
		server.Repository = &model.Repository{
			URL:       ptr.ToString(dbServer.RepositoryUrl),
			Source:    ptr.ToString(dbServer.RepositoryType),
			ID:        ptr.ToString(dbServer.RepositoryID),
			Subfolder: ptr.ToString(dbServer.RepositorySubfolder),
		}
	}

	server.Meta = &upstreamv0.ServerMeta{
		PublisherProvided: make(map[string]any),
	}
	if len(dbServer.UpstreamMeta) > 0 {
		server.Meta.PublisherProvided["upstream_meta"] = dbServer.UpstreamMeta
	}
	if len(dbServer.ServerMeta) > 0 {
		server.Meta.PublisherProvided["server_meta"] = dbServer.ServerMeta
	}
	if dbServer.RepositoryUrl != nil {
		server.Meta.PublisherProvided["repository_url"] = ptr.ToString(dbServer.RepositoryUrl)
	}
	if dbServer.RepositoryID != nil {
		server.Meta.PublisherProvided["repository_id"] = ptr.ToString(dbServer.RepositoryID)
	}
	if dbServer.RepositorySubfolder != nil {
		server.Meta.PublisherProvided["repository_subfolder"] = ptr.ToString(dbServer.RepositorySubfolder)
	}
	if dbServer.RepositoryType != nil {
		server.Meta.PublisherProvided["repository_type"] = ptr.ToString(dbServer.RepositoryType)
	}

	return server
}

func toPackages(
	packages []sqlc.ListServerPackagesRow,
) []model.Package {
	result := make([]model.Package, len(packages))
	for i, dbPackage := range packages {
		result[i] = model.Package{
			RegistryType:    dbPackage.RegistryType,
			RegistryBaseURL: dbPackage.PkgRegistryUrl,
			Identifier:      dbPackage.PkgIdentifier,
			Version:         dbPackage.PkgVersion,
			FileSHA256:      ptr.ToString(dbPackage.Sha256Hash),
			RunTimeHint:     ptr.ToString(dbPackage.RuntimeHint),
			Transport: model.Transport{
				Type:    dbPackage.Transport,
				URL:     ptr.ToString(dbPackage.TransportUrl),
				Headers: toKeyValueInputs(dbPackage.TransportHeaders),
			},
			RuntimeArguments:     toArguments(dbPackage.RuntimeArguments),
			PackageArguments:     toArguments(dbPackage.PackageArguments),
			EnvironmentVariables: toKeyValueInputs(dbPackage.EnvVars),
		}
	}
	return result
}

func toRemotes(
	remotes []sqlc.McpServerRemote,
) []model.Transport {
	result := make([]model.Transport, len(remotes))
	for i, remote := range remotes {
		result[i] = model.Transport{
			Type:    remote.Transport,
			URL:     remote.TransportUrl,
			Headers: toKeyValueInputs(remote.TransportHeaders),
		}
	}
	return result
}

func toKeyValueInputs(
	jsonData []byte,
) []model.KeyValueInput {
	if len(jsonData) == 0 {
		return []model.KeyValueInput{}
	}

	var result []model.KeyValueInput
	if err := json.Unmarshal(jsonData, &result); err != nil {
		// If we can't parse as KeyValueInput array, return empty slice
		// This handles the case where data might be malformed
		return []model.KeyValueInput{}
	}
	return result
}

func toArguments(
	strings []string,
) []model.Argument {
	result := make([]model.Argument, len(strings))
	for i, str := range strings {
		result[i] = model.Argument{
			Name: str,
		}
	}
	return result
}

// Reverse conversion helpers (API -> Database)

func extractArgumentValues(arguments []model.Argument) []string {
	result := make([]string, len(arguments))
	for i, arg := range arguments {
		result[i] = arg.Name
	}
	return result
}

// serializeKeyValueInputs serializes KeyValueInput slice to JSON bytes for database storage
func serializeKeyValueInputs(kvInputs []model.KeyValueInput) ([]byte, error) {
	if len(kvInputs) == 0 {
		return []byte("[]"), nil
	}

	bytes, err := json.Marshal(kvInputs)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize key-value inputs: %w", err)
	}

	return bytes, nil
}

// serializePublisherProvidedMeta serializes the PublisherProvided map to JSON bytes for storage
func serializePublisherProvidedMeta(meta *upstreamv0.ServerMeta) ([]byte, error) {
	if meta == nil || meta.PublisherProvided == nil || len(meta.PublisherProvided) == 0 {
		return nil, nil
	}

	// Serialize the entire PublisherProvided map to JSON
	bytes, err := json.Marshal(meta.PublisherProvided)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize publisher provided metadata: %w", err)
	}

	return bytes, nil
}
