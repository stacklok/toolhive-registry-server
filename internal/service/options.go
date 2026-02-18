package service

import (
	"fmt"
	"time"

	upstreamv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
)

// Option is a function that sets an option for service operations
type Option func(T any) error

type nameOption interface {
	setName(name string) error
}

type versionOption interface {
	setVersion(version string) error
}

type cursorOption interface {
	setCursor(cursor string) error
}

type limitOption interface {
	setLimit(limit int) error
}

type searchOption interface {
	setSearch(search string) error
}

type updatedSinceOption interface {
	setUpdatedSince(updatedSince time.Time) error
}

type nextOption interface {
	setNext(next time.Time) error
}

type prevOption interface {
	setPrev(prev time.Time) error
}

type namespaceOption interface {
	setNamespace(namespace string) error
}

type registryNameOption interface {
	setRegistryName(registryName string) error
}

type serverDataOption interface {
	setServerData(serverData *upstreamv0.ServerJSON) error
}

// WithCursor sets the cursor for the ListServers operation
func WithCursor(cursor string) Option {
	return func(o any) error {
		if cursor == "" {
			return fmt.Errorf("invalid cursor: %s", cursor)
		}

		switch o := o.(type) {
		case cursorOption:
			return o.setCursor(cursor)
		default:
			return fmt.Errorf("invalid option type: %T", o)
		}
	}
}

// WithSearch sets the search for the ListServers operation
func WithSearch(search string) Option {
	return func(o any) error {
		if search == "" {
			return fmt.Errorf("invalid search: %s", search)
		}

		switch o := o.(type) {
		case searchOption:
			return o.setSearch(search)
		default:
			return fmt.Errorf("invalid option type: %T", o)
		}
	}
}

// WithUpdatedSince sets the updated since for the ListServers operation
func WithUpdatedSince(updatedSince time.Time) Option {
	return func(o any) error {
		if updatedSince.IsZero() {
			return fmt.Errorf("invalid updated since: %s", updatedSince)
		}

		switch o := o.(type) {
		case updatedSinceOption:
			return o.setUpdatedSince(updatedSince)
		default:
			return fmt.Errorf("invalid option type: %T", o)
		}
	}
}

// WithRegistryName sets the registry name for the ListServers, ListServerVersions,
// GetServerVersion, PublishServerVersion, or DeleteServerVersion operation
func WithRegistryName(registryName string) Option {
	return func(o any) error {
		if registryName == "" {
			return fmt.Errorf("invalid registry name: %s", registryName)
		}

		switch o := o.(type) {
		case registryNameOption:
			return o.setRegistryName(registryName)
		default:
			return fmt.Errorf("invalid option type: %T", o)
		}
	}
}

// WithNamespace sets the namespace for the ListSkills, GetLatestSkillVersion,
// or DeleteSkillVersion operation
func WithNamespace(namespace string) Option {
	return func(o any) error {
		if namespace == "" {
			return fmt.Errorf("invalid namespace: %s", namespace)
		}

		switch o := o.(type) {
		case namespaceOption:
			return o.setNamespace(namespace)
		default:
			return fmt.Errorf("invalid option type: %T", o)
		}
	}
}

// WithNext sets the next time for the ListServerVersions operation
func WithNext(next time.Time) Option {
	return func(o any) error {
		if next.IsZero() {
			return fmt.Errorf("invalid next: %s", next)
		}

		switch o := o.(type) {
		case nextOption:
			return o.setNext(next)
		default:
			return fmt.Errorf("invalid option type: %T", o)
		}
	}
}

// WithPrev sets the prev time for the ListServerVersions operation
func WithPrev(prev time.Time) Option {
	return func(o any) error {
		if prev.IsZero() {
			return fmt.Errorf("invalid prev: %s", prev)
		}

		switch o := o.(type) {
		case prevOption:
			return o.setPrev(prev)
		default:
			return fmt.Errorf("invalid option type: %T", o)
		}
	}
}

// WithVersion sets the version for the ListServers, GetServerVersion,
// or DeleteServerVersion operation
func WithVersion(version string) Option {
	return func(o any) error {
		if version == "" {
			return fmt.Errorf("invalid version: %s", version)
		}

		switch o := o.(type) {
		case versionOption:
			return o.setVersion(version)
		default:
			return fmt.Errorf("invalid option type: %T", o)
		}
	}
}

// WithName sets the name for the ListServerVersions, GetServerVersion,
// or DeleteServerVersion operation
func WithName(name string) Option {
	return func(o any) error {
		if name == "" {
			return fmt.Errorf("invalid name: %s", name)
		}

		switch o := o.(type) {
		case nameOption:
			return o.setName(name)
		default:
			return fmt.Errorf("invalid option type: %T", o)
		}
	}
}

// WithLimit sets the limit for the ListServers or ListServerVersions operation
func WithLimit(limit int) Option {
	return func(o any) error {
		if limit <= 0 {
			return fmt.Errorf("invalid limit: %d", limit)
		}

		switch o := o.(type) {
		case limitOption:
			return o.setLimit(limit)
		default:
			return fmt.Errorf("invalid option type: %T", o)
		}
	}
}

// WithServerData sets the server data for the PublishServerVersion operation
func WithServerData(serverData *upstreamv0.ServerJSON) Option {
	return func(o any) error {
		if serverData == nil {
			return fmt.Errorf("server data is required")
		}

		switch o := o.(type) {
		case serverDataOption:
			return o.setServerData(serverData)
		default:
			return fmt.Errorf("invalid option type: %T", o)
		}
	}
}
