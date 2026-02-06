package kubernetes

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	mcpv1alpha1 "github.com/stacklok/toolhive/cmd/thv-operator/api/v1alpha1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	"github.com/stacklok/toolhive-registry-server/internal/sync/writer"
)

const (
	defaultRegistryExportAnnotation          = "toolhive.stacklok.dev/registry-export"
	defaultRegistryURLAnnotation             = "toolhive.stacklok.dev/registry-url"
	defaultRegistryDescriptionAnnotation     = "toolhive.stacklok.dev/registry-description"
	defaultRegistryToolDefinitionsAnnotation = "toolhive.stacklok.dev/tool-definitions"
	defaultRegistryToolsAnnotation           = "toolhive.stacklok.dev/tools"

	defaultRequeueAfter = 10 * time.Second

	leaderElectionID = "toolhive-registry-server-leader-election"

	// namespaceRegexPattern is the regex pattern for a valid Kubernetes namespace name
	namespaceRegexPattern = `^[a-z0-9]([-a-z0-9]{0,61}[a-z0-9])?$`
)

var (
	// serviceAccountNamespaceFile is the path to the file containing the namespace
	// of the service account running in a Kubernetes pod.
	// This is a variable to allow tests to override it.
	serviceAccountNamespaceFile = "/var/run/secrets/kubernetes.io/serviceaccount/namespace"
)

// hasRequiredRegistryAnnotations checks if the given annotations map contains
// all required annotations for registry export (description and URL).
func hasRequiredRegistryAnnotations(annotations map[string]string) bool {
	if annotations == nil {
		return false
	}
	_, hasDesc := annotations[defaultRegistryDescriptionAnnotation]
	_, hasURL := annotations[defaultRegistryURLAnnotation]
	return hasDesc && hasURL
}

type mcpServerReconcilerOptions struct {
	namespaces   []string
	requeueAfter time.Duration
	syncWriter   writer.SyncWriter
	registryName string
}

// Option is a function that sets an option for the MCPServerReconciler.
type Option func(*mcpServerReconcilerOptions) error

// WithNamespaces sets the namespaces to watch.
func WithNamespaces(namespaces ...string) Option {
	return func(o *mcpServerReconcilerOptions) error {
		if err := validateNamespaces(namespaces); err != nil {
			// Don't wrap error, return it directly
			return err
		}

		if o.namespaces == nil {
			o.namespaces = make([]string, 0)
		}
		o.namespaces = append(o.namespaces, namespaces...)
		return nil
	}
}

// WithCurrentNamespace configures the reconciler to watch the namespace
// of the current Kubernetes pod by reading from the service account namespace file.
func WithCurrentNamespace() Option {
	return func(o *mcpServerReconcilerOptions) error {
		namespace, err := readNamespaceFromFile(serviceAccountNamespaceFile)
		if err != nil {
			return err
		}
		if o.namespaces == nil {
			o.namespaces = make([]string, 0)
		}
		o.namespaces = append(o.namespaces, namespace)
		return nil
	}
}

// readNamespaceFromFile reads a Kubernetes namespace from the specified file path.
// It reads at most 256 bytes since Kubernetes namespace names have a maximum length
// of 253 characters as per DNS subdomain naming rules.
// See: https://kubernetes.io/docs/concepts/overview/working-with-objects/names/#dns-subdomain-names
func readNamespaceFromFile(path string) (string, error) {
	f, err := os.Open(filepath.Clean(path))
	if err != nil {
		return "", fmt.Errorf("failed to open namespace file: %w", err)
	}
	defer f.Close()

	// Read up to 257 bytes to detect if file exceeds the 256 byte limit
	buf := make([]byte, 257)
	n, err := f.Read(buf)
	if err != nil && !errors.Is(err, io.EOF) {
		return "", fmt.Errorf("failed to read namespace: %w", err)
	}
	if n > 256 {
		return "", fmt.Errorf("namespace file exceeds maximum size of 256 bytes")
	}
	return strings.TrimSpace(string(buf[:n])), nil
}

// WithRequeueAfter sets the requeue after duration.
func WithRequeueAfter(requeueAfter time.Duration) Option {
	return func(o *mcpServerReconcilerOptions) error {
		if requeueAfter <= 0 {
			return fmt.Errorf("requeueAfter must be greater than 0")
		}
		o.requeueAfter = requeueAfter
		return nil
	}
}

// WithSyncWriter sets the sync writer.
func WithSyncWriter(sw writer.SyncWriter) Option {
	return func(o *mcpServerReconcilerOptions) error {
		if sw == nil {
			return fmt.Errorf("sync writer is required")
		}
		o.syncWriter = sw
		return nil
	}
}

// WithRegistryName sets the registry name. This is used to identify the
// registry in the sync writer.
func WithRegistryName(name string) Option {
	return func(o *mcpServerReconcilerOptions) error {
		if name == "" {
			return fmt.Errorf("registry name is required")
		}
		o.registryName = name
		return nil
	}
}

// NewMCPServerReconciler creates a new MCPServerReconciler.
func NewMCPServerReconciler(
	ctx context.Context,
	opts ...Option,
) (ctrl.Manager, error) {
	o := &mcpServerReconcilerOptions{
		namespaces:   []string{},
		requeueAfter: defaultRequeueAfter,
	}

	for _, opt := range opts {
		if err := opt(o); err != nil {
			return nil, err
		}
	}

	if o.syncWriter == nil {
		return nil, fmt.Errorf("sync writer is required")
	}
	if o.registryName == "" {
		return nil, fmt.Errorf("registry name is required")
	}

	// This is validated in the options, so we can safely use the namespaces.
	defaultNamespaces := map[string]cache.Config{}
	for _, namespace := range o.namespaces {
		slog.Info("Watching namespace", "namespace", namespace)
		defaultNamespaces[namespace] = cache.Config{}
	}

	scheme := runtime.NewScheme()
	if err := mcpv1alpha1.AddToScheme(scheme); err != nil {
		return nil, fmt.Errorf("failed to add MCPv1alpha1 scheme: %w", err)
	}

	options := ctrl.Options{
		Scheme:           scheme,
		LeaderElection:   true,
		LeaderElectionID: leaderElectionID,
		// disable metrics server
		Metrics: metricsserver.Options{
			BindAddress: "0",
		},
	}

	if len(defaultNamespaces) > 0 {
		options.Cache = cache.Options{
			DefaultNamespaces: defaultNamespaces,
		}
	}

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), options)
	if err != nil {
		return nil, fmt.Errorf("failed to create manager: %w", err)
	}

	controller := &MCPServerReconciler{
		requeueAfter: o.requeueAfter,
		syncWriter:   o.syncWriter,
		registryName: o.registryName,
	}

	if err := controller.SetupWithManager(mgr); err != nil {
		return nil, fmt.Errorf("failed to setup controller with manager: %w", err)
	}

	go func() {
		if err := mgr.Start(ctx); err != nil {
			slog.Error("Failed to start manager", "error", err)
		}
	}()

	return mgr, nil
}

func validateNamespaces(namespaces []string) error {
	for _, namespace := range namespaces {
		if !regexp.MustCompile(namespaceRegexPattern).MatchString(namespace) {
			return fmt.Errorf("invalid namespace name: %s", namespace)
		}
	}
	return nil
}
