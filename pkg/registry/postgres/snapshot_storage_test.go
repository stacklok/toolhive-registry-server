package postgres_test

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
	"time"

	upstreammodel "github.com/modelcontextprotocol/registry/pkg/model"
	thvregistry "github.com/stacklok/toolhive-core/registry/types"

	"github.com/stacklok/toolhive-registry-server/pkg/registry/model"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/postgres"
)

// CommitSnapshot applies storage safety, not the strict publication rules:
// synced records v1.5.4 stored are stored, duplicate packages, remotes and
// icons collapse to the first, and content the database rejects is a
// validation failure of the source rather than an unavailable backend.
func TestCommitSnapshotStorageSafety(t *testing.T) {
	t.Parallel()
	d, pool := newBackend(t)
	jobs, err := postgres.NewJobs(pool, 65536)
	if err != nil {
		t.Fatal(err)
	}
	entries, err := postgres.NewEntries(t.Context(), pool, 65536)
	if err != nil {
		t.Fatal(err)
	}
	source, err := d.CreateSource(t.Context(), persistence.SourceDefinition{
		Name: "lenient", API: &persistence.APISpec{Endpoint: "https://example.org"}, Schedule: "1h"})
	if err != nil {
		t.Fatal(err)
	}
	commit := func(snapshot model.Snapshot) error {
		t.Helper()
		job, e := jobs.Acquire(t.Context(), source.Name, time.Minute)
		if e != nil {
			t.Fatal(e)
		}
		e = jobs.CommitSnapshot(t.Context(), job, persistence.SyncResult{Hash: "h"}, snapshot)
		if e != nil {
			if failErr := jobs.Fail(t.Context(), job, persistence.FailureValidation); failErr != nil {
				t.Fatal(failErr)
			}
		}
		return e
	}

	remote := upstreammodel.Transport{Type: "streamable-http", URL: "https://example.org/mcp"}
	pkg := upstreammodel.Package{RegistryType: "oci", Identifier: "example.org/image:1", Transport: upstreammodel.Transport{Type: "stdio"}}
	light := "light"
	var accepted model.Snapshot
	accepted.Data.Servers = []model.Server{
		{Name: "com.example/bar_", Version: "latest"}, // no description, name the strict format rejects
		{Name: "com.example/dups", Version: "1", Description: "dups",
			Packages: []upstreammodel.Package{pkg, pkg},
			Remotes:  []upstreammodel.Transport{remote, remote},
			Icons:    []upstreammodel.Icon{{Src: "https://example.org/i.png"}, {Src: "https://example.org/i.png", Theme: &light}}},
	}
	accepted.Data.Skills = []thvregistry.Skill{{Name: "skill", Version: "1", Status: "deprecated"}}
	if err := commit(accepted); err != nil {
		t.Fatalf("storable snapshot rejected: %v", err)
	}
	got, err := entries.GetEntry(t.Context(), source.ID, persistence.ServerKind, "com.example/dups", persistence.VersionSelector{Exact: "1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Server.Packages) != 1 || len(got.Server.Remotes) != 1 || len(got.Server.Icons) != 1 {
		t.Fatalf("duplicates not collapsed: %d packages, %d remotes, %d icons",
			len(got.Server.Packages), len(got.Server.Remotes), len(got.Server.Icons))
	}
	if _, err = entries.GetEntry(t.Context(), source.ID, persistence.ServerKind, "com.example/bar_", persistence.VersionSelector{Exact: "latest"}); err != nil {
		t.Fatalf("lenient server not stored: %v", err)
	}

	random := make([]byte, 6000)
	if _, err := rand.Read(random); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		server model.Server
		reason string
	}{
		{name: "NUL in description", server: model.Server{Name: "com.example/nul", Version: "1", Description: "a\x00b"},
			reason: `server "com.example/nul" version "1": description`},
		{name: "index row too large", server: model.Server{Name: "com.example/icon", Version: "1",
			Icons: []upstreammodel.Icon{{Src: "data:image/png;base64," + hex.EncodeToString(random)}}},
			reason: "SQLSTATE 54000"},
	} {
		// Sequential on purpose: every case leases the same source.
		var snapshot model.Snapshot
		snapshot.Data.Servers = []model.Server{tc.server}
		err := commit(snapshot)
		if !errors.Is(err, persistence.ErrInvalid) || errors.Is(err, persistence.ErrUnavailable) {
			t.Fatalf("%s: want ErrInvalid, got %v", tc.name, err)
		}
		if !strings.Contains(err.Error(), tc.reason) {
			t.Fatalf("%s: error %q does not contain %q", tc.name, err, tc.reason)
		}
		// The previous snapshot stays visible.
		if _, err = entries.GetEntry(t.Context(), source.ID, persistence.ServerKind, "com.example/dups",
			persistence.VersionSelector{Exact: "1"}); err != nil {
			t.Fatalf("%s: previous data lost: %v", tc.name, err)
		}
	}
}
