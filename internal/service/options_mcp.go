package service

import (
	"time"

	upstreamv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
)

// ListServersOptions is the options for the ListServers operation
type ListServersOptions struct {
	RegistryName string
	Cursor       string
	Limit        int
	Search       string
	UpdatedSince time.Time
	Version      string
	Claims       map[string]any
}

//nolint:unparam
func (o *ListServersOptions) setRegistryName(registryName string) error {
	o.RegistryName = registryName
	return nil
}

//nolint:unparam
func (o *ListServersOptions) setCursor(cursor string) error {
	o.Cursor = cursor
	return nil
}

//nolint:unparam
func (o *ListServersOptions) setLimit(limit int) error {
	o.Limit = limit
	return nil
}

//nolint:unparam
func (o *ListServersOptions) setSearch(search string) error {
	o.Search = search
	return nil
}

//nolint:unparam
func (o *ListServersOptions) setUpdatedSince(updatedSince time.Time) error {
	o.UpdatedSince = updatedSince
	return nil
}

//nolint:unparam
func (o *ListServersOptions) setVersion(version string) error {
	o.Version = version
	return nil
}

//nolint:unparam
func (o *ListServersOptions) setClaims(claims map[string]any) error {
	o.Claims = claims
	return nil
}

// ListServerVersionsOptions is the options for the ListServerVersions operation
type ListServerVersionsOptions struct {
	RegistryName string
	Name         string
	Limit        int
	Claims       map[string]any
}

//nolint:unparam
func (o *ListServerVersionsOptions) setRegistryName(registryName string) error {
	o.RegistryName = registryName
	return nil
}

//nolint:unparam
func (o *ListServerVersionsOptions) setName(name string) error {
	o.Name = name
	return nil
}

//nolint:unparam
func (o *ListServerVersionsOptions) setLimit(limit int) error {
	o.Limit = limit
	return nil
}

//nolint:unparam
func (o *ListServerVersionsOptions) setClaims(claims map[string]any) error {
	o.Claims = claims
	return nil
}

// GetServerVersionOptions is the options for the GetServerVersion operation
type GetServerVersionOptions struct {
	RegistryName string
	SourceName   string
	Name         string
	Version      string
	Claims       map[string]any
}

//nolint:unparam
func (o *GetServerVersionOptions) setRegistryName(registryName string) error {
	o.RegistryName = registryName
	return nil
}

//nolint:unparam
func (o *GetServerVersionOptions) setName(name string) error {
	o.Name = name
	return nil
}

//nolint:unparam
func (o *GetServerVersionOptions) setVersion(version string) error {
	o.Version = version
	return nil
}

//nolint:unparam
func (o *GetServerVersionOptions) setClaims(claims map[string]any) error {
	o.Claims = claims
	return nil
}

// PublishServerVersionOptions is the options for the PublishServerVersion operation
type PublishServerVersionOptions struct {
	ServerData *upstreamv0.ServerJSON
	Claims     map[string]any
	JWTClaims  map[string]any
}

//nolint:unparam
func (o *PublishServerVersionOptions) setServerData(serverData *upstreamv0.ServerJSON) error {
	o.ServerData = serverData
	return nil
}

//nolint:unparam
func (o *PublishServerVersionOptions) setClaims(claims map[string]any) error {
	o.Claims = claims
	return nil
}

//nolint:unparam
func (o *PublishServerVersionOptions) setJWTClaims(claims map[string]any) error {
	o.JWTClaims = claims
	return nil
}

// DeleteServerVersionOptions is the options for the DeleteServerVersion operation
type DeleteServerVersionOptions struct {
	ServerName string
	Version    string
	JWTClaims  map[string]any
}

//nolint:unparam
func (o *DeleteServerVersionOptions) setName(serverName string) error {
	o.ServerName = serverName
	return nil
}

//nolint:unparam
func (o *DeleteServerVersionOptions) setVersion(version string) error {
	o.Version = version
	return nil
}

//nolint:unparam
func (o *DeleteServerVersionOptions) setJWTClaims(claims map[string]any) error {
	o.JWTClaims = claims
	return nil
}
