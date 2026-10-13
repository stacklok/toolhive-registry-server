package conformance

import (
	"testing"

	httpconformance "github.com/stacklok/toolhive-registry-server/pkg/registry/httpapi/conformance"
)

func TestConsumerHTTPMemory(t *testing.T) {
	t.Parallel()
	m := fresh()
	httpconformance.RunConsumer(t, m, m)
}
