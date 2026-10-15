package kubernetes

import (
	"context"
	"errors"

	mcp "github.com/stacklok/toolhive/cmd/thv-operator/api/v1beta1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/stacklok/toolhive-registry-server/pkg/registry/ingest"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/operator"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/reconcile"
)

// ErrRetiredExportClaims is a bounded remediation diagnostic, never annotation
// values or raw Kubernetes errors. Presence (even empty/malformed) is significant.
var ErrRetiredExportClaims = errors.New("exported operator resource has retired authz-claims annotation; " +
	"review exposure and explicitly remove it before standalone cutover")

// NewStandaloneOperator constructs the compatibility boundary around the public
// runner, not a manager constructor. The authoritative client remains caller-owned.
// report receives only the bounded retired-annotation diagnostic; it must not block.
func NewStandaloneOperator(
	api client.WithWatch, jobs persistence.Jobs, definition persistence.SourceDefinition,
	options reconcile.Options, report func(error),
) (*operator.Runner, error) {
	if api == nil {
		return nil, persistence.ErrInvalid
	}
	return operator.NewRunner(&standaloneExportClient{WithWatch: api, report: report}, jobs, definition, options)
}

// PreflightStandaloneOperator lists all persisted effective namespaces/kinds.
// It does not watch, lease, write, initialize storage, or convert export metadata.
func PreflightStandaloneOperator(ctx context.Context, api client.WithWatch, definition persistence.SourceDefinition) error {
	if api == nil {
		return persistence.ErrInvalid
	}
	if definition.ID == "" {
		definition.ID = "preflight"
	}
	checked := &standaloneExportClient{WithWatch: api}
	if _, err := operator.NewFetcher(checked, definition); err != nil {
		return err
	}
	namespaces := definition.Kubernetes.Namespaces
	if len(namespaces) == 0 {
		namespaces = []string{""}
	}
	for _, namespace := range namespaces {
		for _, list := range []client.ObjectList{&mcp.MCPServerList{}, &mcp.VirtualMCPServerList{}, &mcp.MCPRemoteProxyList{}} {
			if err := checked.List(ctx, list, client.InNamespace(namespace)); err != nil {
				return err
			}
		}
	}
	return nil
}

// Delegation preserves watches and all other client operations. Checking the
// actual list before conversion prevents missing metadata from hiding claims.
type standaloneExportClient struct {
	client.WithWatch
	report func(error)
}

func (c *standaloneExportClient) List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
	if err := c.WithWatch.List(ctx, list, opts...); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return ingest.ErrFetch
	}
	if list.GetContinue() != "" {
		return ingest.ErrFetch
	}
	switch list.(type) {
	case *mcp.MCPServerList, *mcp.VirtualMCPServerList, *mcp.MCPRemoteProxyList:
	default:
		return nil
	}
	return meta.EachListItem(list, func(obj runtime.Object) error {
		object, ok := obj.(client.Object)
		if !ok {
			return persistence.ErrInvalid
		}
		annotations := object.GetAnnotations()
		if annotations["toolhive.stacklok.dev/registry-export"] != "true" {
			return nil
		}
		if _, present := annotations["toolhive.stacklok.dev/authz-claims"]; present {
			if c.report != nil {
				c.report(ErrRetiredExportClaims)
			}
			return ErrRetiredExportClaims
		}
		return nil
	})
}
