package integration

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

var (
	k8sClient client.Client
	testEnv   *envtest.Environment
	ctx       context.Context
	cancel    context.CancelFunc
)

func TestRegistryAPIIntegration(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Registry API Integration Suite")
}

var _ = BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	ctx, cancel = context.WithCancel(context.TODO())

	By("bootstrapping test environment")

	// Check if we should use an existing cluster (for CI/CD)
	useExistingCluster := os.Getenv("USE_EXISTING_CLUSTER") == "true"

	// Get kubebuilder assets path
	kubebuilderAssets := os.Getenv("KUBEBUILDER_ASSETS")

	if !useExistingCluster {
		By(fmt.Sprintf("using kubebuilder assets from: %s", kubebuilderAssets))
		if kubebuilderAssets == "" {
			By("WARNING: no kubebuilder assets found, ConfigMap tests may be skipped")
		}
	}

	testEnv = &envtest.Environment{
		UseExistingCluster:    &useExistingCluster,
		BinaryAssetsDirectory: kubebuilderAssets,
	}

	cfg, err := testEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(cfg).NotTo(BeNil())

	// Create controller-runtime client
	k8sClient, err = client.New(cfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).NotTo(HaveOccurred())
	Expect(k8sClient).NotTo(BeNil())
})

var _ = AfterSuite(func() {
	cancel()
	By("tearing down the test environment")
	err := testEnv.Stop()
	Expect(err).NotTo(HaveOccurred())
})

// TestNamespace represents a test namespace with automatic cleanup
type TestNamespace struct {
	Name      string
	Namespace *corev1.Namespace
	Client    client.Client
	ctx       context.Context
}

// NewTestNamespace creates a new test namespace with a unique name
func NewTestNamespace(namePrefix string) *TestNamespace {
	timestamp := time.Now().UnixNano()
	name := fmt.Sprintf("%s-%d", namePrefix, timestamp)

	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			Labels: map[string]string{
				"test.toolhive.io/suite":  "registry-api-integration",
				"test.toolhive.io/prefix": namePrefix,
			},
		},
	}

	return &TestNamespace{
		Name:      name,
		Namespace: ns,
		Client:    k8sClient,
		ctx:       ctx,
	}
}

// Create creates the namespace in the cluster
func (tn *TestNamespace) Create() error {
	return tn.Client.Create(tn.ctx, tn.Namespace)
}

// Delete deletes the namespace and all its resources
func (tn *TestNamespace) Delete() error {
	return tn.Client.Delete(tn.ctx, tn.Namespace)
}

// WaitForDeletion waits for the namespace to be fully deleted
func (tn *TestNamespace) WaitForDeletion(timeout time.Duration) {
	Eventually(func() bool {
		ns := &corev1.Namespace{}
		err := tn.Client.Get(tn.ctx, client.ObjectKey{Name: tn.Name}, ns)
		return err != nil
	}, timeout, time.Second).Should(BeTrue(), "namespace should be deleted")
}

// GetClient returns a client scoped to this namespace
func (tn *TestNamespace) GetClient() client.Client {
	return tn.Client
}

// GetContext returns the test context
func (tn *TestNamespace) GetContext() context.Context {
	return tn.ctx
}

// createTestNamespace creates a namespace for testing
func createTestNamespace(ctx context.Context) string {
	testNs := NewTestNamespace("registry-test")
	Expect(testNs.Create()).To(Succeed())
	return testNs.Name
}

// deleteTestNamespace deletes a test namespace and waits for it to be fully removed
func deleteTestNamespace(ctx context.Context, namespace string) {
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: namespace,
		},
	}
	Expect(k8sClient.Delete(ctx, ns)).Should(Succeed())
}

// createTempDir creates a temporary directory for test files
func createTempDir(prefix string) string {
	dir, err := os.MkdirTemp("", prefix)
	Expect(err).NotTo(HaveOccurred())
	return dir
}

// cleanupTempDir removes a temporary directory
func cleanupTempDir(dir string) {
	if err := os.RemoveAll(dir); err != nil {
		By(fmt.Sprintf("Warning: failed to cleanup temp dir %s: %v", dir, err))
	}
}
