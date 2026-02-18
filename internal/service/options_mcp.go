package service

import (
	"time"

	upstreamv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
)

// ListServersOptions is the options for the ListServers operation
type ListServersOptions struct {
	RegistryName *string
	Cursor       string
	Limit        int
	Search       string
	UpdatedSince time.Time
	Version      string
}

//nolint:unparam
func (o *ListServersOptions) setRegistryName(registryName string) error {
	o.RegistryName = &registryName
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

// ListServerVersionsOptions is the options for the ListServerVersions operation
type ListServerVersionsOptions struct {
	RegistryName *string
	Name         string
	Next         *time.Time
	Prev         *time.Time
	Limit        int
}

//nolint:unparam
func (o *ListServerVersionsOptions) setRegistryName(registryName string) error {
	o.RegistryName = &registryName
	return nil
}

//nolint:unparam
func (o *ListServerVersionsOptions) setName(name string) error {
	o.Name = name
	return nil
}

//nolint:unparam
func (o *ListServerVersionsOptions) setNext(next time.Time) error {
	o.Next = &next
	return nil
}

//nolint:unparam
func (o *ListServerVersionsOptions) setPrev(prev time.Time) error {
	o.Prev = &prev
	return nil
}

//nolint:unparam
func (o *ListServerVersionsOptions) setLimit(limit int) error {
	o.Limit = limit
	return nil
}

// GetServerVersionOptions is the options for the GetServerVersion operation
type GetServerVersionOptions struct {
	RegistryName string
	Name         string
	Version      string
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

// PublishServerVersionOptions is the options for the PublishServerVersion operation
type PublishServerVersionOptions struct {
	RegistryName string
	ServerData   *upstreamv0.ServerJSON
}

//nolint:unparam
func (o *PublishServerVersionOptions) setRegistryName(registryName string) error {
	o.RegistryName = registryName
	return nil
}

//nolint:unparam
func (o *PublishServerVersionOptions) setServerData(serverData *upstreamv0.ServerJSON) error {
	o.ServerData = serverData
	return nil
}

// DeleteServerVersionOptions is the options for the DeleteServerVersion operation
type DeleteServerVersionOptions struct {
	RegistryName string
	ServerName   string
	Version      string
}

//nolint:unparam
func (o *DeleteServerVersionOptions) setRegistryName(registryName string) error {
	o.RegistryName = registryName
	return nil
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
