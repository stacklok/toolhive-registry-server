package integration

import (
	"context"
	"fmt"
	"os"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

var (
	ctx    context.Context
	cancel context.CancelFunc
)

func TestRegistryAPIIntegration(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Registry API Integration Suite")
}

var _ = BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	ctx, cancel = context.WithCancel(context.TODO())
})

var _ = AfterSuite(func() {
	cancel()
})

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
