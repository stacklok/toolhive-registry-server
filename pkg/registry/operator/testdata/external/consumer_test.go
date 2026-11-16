package consumer

import (
	"testing"
	"time"

	"github.com/stacklok/toolhive-registry-server/pkg/registry/operator"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/persistence"
	"github.com/stacklok/toolhive-registry-server/pkg/registry/reconcile"
	mcp "github.com/stacklok/toolhive/cmd/thv-operator/api/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

// Register borrows a host manager; the host calls mgr.Start(ctx) and owns leader election.
func Register(mgr manager.Manager, jobs persistence.Jobs, source persistence.SourceDefinition) (*operator.Runner, error) {
	api, err := client.NewWithWatch(mgr.GetConfig(), client.Options{Scheme: mgr.GetScheme()})
	if err != nil {
		return nil, err
	}
	runner, err := operator.NewRunner(api, jobs, source, reconcile.Options{PollInterval: 10 * time.Second, LeaseDuration: time.Minute})
	if err != nil {
		return nil, err
	}
	if err := runner.Register(mgr); err != nil {
		return nil, err
	}
	return runner, nil
}

func TestHostFetch(t *testing.T) {
	s := runtime.NewScheme()
	if err := mcp.AddToScheme(s); err != nil {
		t.Fatal(err)
	}
	obj := &mcp.VirtualMCPServer{ObjectMeta: metav1.ObjectMeta{Name: "example", Namespace: "default", Annotations: map[string]string{"toolhive.stacklok.dev/registry-export": "true", "toolhive.stacklok.dev/registry-url": "https://example.org/mcp", "toolhive.stacklok.dev/registry-description": "Example"}}}
	api := fake.NewClientBuilder().WithScheme(s).WithObjects(obj).Build()
	source := persistence.SourceDefinition{ID: "host-source", Name: "operator", Kubernetes: &persistence.KubernetesSpec{Namespaces: []string{"default"}}}
	fetcher, err := operator.NewFetcher(api, source)
	if err != nil {
		t.Fatal(err)
	}
	result, err := fetcher.Fetch(t.Context(), source)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Snapshot.Data.Servers) != 1 {
		t.Fatal("missing exported resource")
	}
	if result.Snapshot.Data.Servers[0].Remotes[0].URL != "https://example.org/mcp" {
		t.Fatal("export URL changed")
	}
}
