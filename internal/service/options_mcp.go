package service

import (
	upstreamv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
)

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
