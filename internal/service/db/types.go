package database

import (
	"time"

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
	packages []sqlc.McpServerPackage,
	remotes []sqlc.McpServerRemote,
) upstreamv0.ServerJSON {
	server := upstreamv0.ServerJSON{
		Schema:      "https://static.modelcontextprotocol.io/schemas/2025-10-17/server.schema.json",
		Name:        dbServer.Name,
		Description: *dbServer.Description,
		Title:       *dbServer.Title,
		Repository: &model.Repository{
			URL:       *dbServer.RepositoryUrl,
			Source:    *dbServer.RepositoryType,
			ID:        *dbServer.RepositoryID,
			Subfolder: *dbServer.RepositorySubfolder,
		},
		Version:    dbServer.Version,
		WebsiteURL: *dbServer.Website,
		Packages:   toPackages(packages),
		Remotes:    toRemotes(remotes),
	}

	server.Meta = &upstreamv0.ServerMeta{
		PublisherProvided: make(map[string]any),
	}
	server.Meta.PublisherProvided["upstream_meta"] = dbServer.UpstreamMeta
	server.Meta.PublisherProvided["server_meta"] = dbServer.ServerMeta
	server.Meta.PublisherProvided["repository_url"] = dbServer.RepositoryUrl
	server.Meta.PublisherProvided["repository_id"] = dbServer.RepositoryID
	server.Meta.PublisherProvided["repository_subfolder"] = dbServer.RepositorySubfolder
	server.Meta.PublisherProvided["repository_type"] = dbServer.RepositoryType

	return server
}

func toPackages(
	packages []sqlc.McpServerPackage,
) []model.Package {
	result := make([]model.Package, len(packages))
	for i, dbPackage := range packages {
		result[i] = model.Package{
			RegistryType:    dbPackage.RegistryType,
			RegistryBaseURL: dbPackage.PkgRegistryUrl,
			Identifier:      dbPackage.PkgIdentifier,
			Version:         dbPackage.PkgVersion,
			FileSHA256:      *dbPackage.Sha256Hash,
			RunTimeHint:     *dbPackage.RuntimeHint,
			Transport: model.Transport{
				Type:    dbPackage.Transport,
				URL:     *dbPackage.TransportUrl,
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
	strings []string,
) []model.KeyValueInput {
	result := make([]model.KeyValueInput, len(strings))
	for i, string := range strings {
		result[i] = model.KeyValueInput{
			Name: string,
		}
	}
	return result
}

func toArguments(
	strings []string,
) []model.Argument {
	result := make([]model.Argument, len(strings))
	for i, string := range strings {
		result[i] = model.Argument{
			Name: string,
		}
	}
	return result
}
