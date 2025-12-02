package kubernetes

import (
	"testing"

	mcpv1alpha1 "github.com/stacklok/toolhive/cmd/thv-operator/api/v1alpha1"
	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/event"
)

// createTestMCPServerForPredicate creates a test MCPServer object with the given annotations for predicate tests
func createTestMCPServerForPredicate(annotations map[string]string) *mcpv1alpha1.MCPServer {
	return &mcpv1alpha1.MCPServer{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "test-server",
			Namespace:   "default",
			Annotations: annotations,
		},
		Spec: mcpv1alpha1.MCPServerSpec{
			Image:     "test/image:latest",
			Transport: "stdio",
		},
	}
}

func TestMakeNewObjectPredicate(t *testing.T) {
	t.Parallel()

	//nolint:goconst
	annotation := "test.annotation/export"

	tests := []struct {
		name        string
		annotations map[string]string
		want        bool
	}{
		{
			name:        "annotation set to true",
			annotations: map[string]string{"test.annotation/export": "true"},
			want:        true,
		},
		{
			name:        "annotation set to false",
			annotations: map[string]string{"test.annotation/export": "false"},
			want:        false,
		},
		{
			name:        "annotation with other value",
			annotations: map[string]string{"test.annotation/export": "other"},
			want:        false,
		},
		{
			name:        "annotation missing",
			annotations: map[string]string{"other.annotation": "true"},
			want:        false,
		},
		{
			name:        "nil annotations",
			annotations: nil,
			want:        false,
		},
		{
			name:        "empty annotations map",
			annotations: map[string]string{},
			want:        false,
		},
		{
			name: "annotation true with other annotations",
			annotations: map[string]string{
				"test.annotation/export": "true",
				"other.annotation":       "value",
			},
			want: true,
		},
		{
			name: "annotation false with other annotations",
			annotations: map[string]string{
				"test.annotation/export": "false",
				"other.annotation":       "value",
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			predicate := makeNewObjectPredicate[*mcpv1alpha1.MCPServer](annotation)
			obj := createTestMCPServerForPredicate(tt.annotations)
			createEvent := event.TypedCreateEvent[*mcpv1alpha1.MCPServer]{
				Object: obj,
			}

			result := predicate(createEvent)
			assert.Equal(t, tt.want, result)
		})
	}
}

func TestMakeUpdateObjectPredicate(t *testing.T) {
	t.Parallel()

	annotation := "test.annotation/export"

	tests := []struct {
		name        string
		oldAnnots   map[string]string
		newAnnots   map[string]string
		want        bool
		description string
	}{
		{
			name:        "new enabled, old enabled",
			oldAnnots:   map[string]string{"test.annotation/export": "true"},
			newAnnots:   map[string]string{"test.annotation/export": "true"},
			want:        true,
			description: "both enabled should enqueue",
		},
		{
			name:        "new enabled, old disabled",
			oldAnnots:   map[string]string{"test.annotation/export": "false"},
			newAnnots:   map[string]string{"test.annotation/export": "true"},
			want:        true,
			description: "new enabled should enqueue",
		},
		{
			name:        "new disabled, old enabled",
			oldAnnots:   map[string]string{"test.annotation/export": "true"},
			newAnnots:   map[string]string{"test.annotation/export": "false"},
			want:        true,
			description: "old enabled should enqueue",
		},
		{
			name:        "new disabled, old disabled",
			oldAnnots:   map[string]string{"test.annotation/export": "false"},
			newAnnots:   map[string]string{"test.annotation/export": "false"},
			want:        false,
			description: "both disabled should ignore",
		},
		{
			name:        "new enabled, old missing",
			oldAnnots:   map[string]string{},
			newAnnots:   map[string]string{"test.annotation/export": "true"},
			want:        true,
			description: "new enabled should enqueue",
		},
		{
			name:        "new missing, old enabled",
			oldAnnots:   map[string]string{"test.annotation/export": "true"},
			newAnnots:   map[string]string{},
			want:        true,
			description: "old enabled should enqueue",
		},
		{
			name:        "new missing, old missing",
			oldAnnots:   map[string]string{},
			newAnnots:   map[string]string{},
			want:        false,
			description: "both missing should ignore",
		},
		{
			name:        "new nil annotations, old enabled",
			oldAnnots:   map[string]string{"test.annotation/export": "true"},
			newAnnots:   nil,
			want:        true,
			description: "old enabled should enqueue",
		},
		{
			name:        "new enabled, old nil annotations",
			oldAnnots:   nil,
			newAnnots:   map[string]string{"test.annotation/export": "true"},
			want:        true,
			description: "new enabled should enqueue",
		},
		{
			name:        "new nil annotations, old nil annotations",
			oldAnnots:   nil,
			newAnnots:   nil,
			want:        false,
			description: "both nil should ignore",
		},
		{
			name:        "new enabled with other value, old disabled",
			oldAnnots:   map[string]string{"test.annotation/export": "false"},
			newAnnots:   map[string]string{"test.annotation/export": "other"},
			want:        false,
			description: "new not true should not enqueue if old also not enabled",
		},
		{
			name:        "new other value, old enabled",
			oldAnnots:   map[string]string{"test.annotation/export": "true"},
			newAnnots:   map[string]string{"test.annotation/export": "other"},
			want:        true,
			description: "old enabled should enqueue even if new is not true",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			predicate := makeUpdateObjectPredicate[*mcpv1alpha1.MCPServer](annotation)
			oldObj := createTestMCPServerForPredicate(tt.oldAnnots)
			newObj := createTestMCPServerForPredicate(tt.newAnnots)
			updateEvent := event.TypedUpdateEvent[*mcpv1alpha1.MCPServer]{
				ObjectOld: oldObj,
				ObjectNew: newObj,
			}

			result := predicate(updateEvent)
			assert.Equal(t, tt.want, result, tt.description)
		})
	}
}

func TestMakeDeleteObjectPredicate(t *testing.T) {
	t.Parallel()

	annotation := "test.annotation/export"

	tests := []struct {
		name        string
		annotations map[string]string
		want        bool
	}{
		{
			name:        "annotation set to true",
			annotations: map[string]string{"test.annotation/export": "true"},
			want:        true,
		},
		{
			name:        "annotation set to false",
			annotations: map[string]string{"test.annotation/export": "false"},
			want:        false,
		},
		{
			name:        "annotation with other value",
			annotations: map[string]string{"test.annotation/export": "other"},
			want:        false,
		},
		{
			name:        "annotation missing",
			annotations: map[string]string{"other.annotation": "true"},
			want:        false,
		},
		{
			name:        "nil annotations",
			annotations: nil,
			want:        false,
		},
		{
			name:        "empty annotations map",
			annotations: map[string]string{},
			want:        false,
		},
		{
			name: "annotation true with other annotations",
			annotations: map[string]string{
				"test.annotation/export": "true",
				"other.annotation":       "value",
			},
			want: true,
		},
		{
			name: "annotation false with other annotations",
			annotations: map[string]string{
				"test.annotation/export": "false",
				"other.annotation":       "value",
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			predicate := makeDeleteObjectPredicate[*mcpv1alpha1.MCPServer](annotation)
			obj := createTestMCPServerForPredicate(tt.annotations)
			deleteEvent := event.TypedDeleteEvent[*mcpv1alpha1.MCPServer]{
				Object: obj,
			}

			result := predicate(deleteEvent)
			assert.Equal(t, tt.want, result)
		})
	}
}

func TestCheckAnnotation(t *testing.T) {
	t.Parallel()

	annotation := "test.annotation/export"

	tests := []struct {
		name        string
		annotations map[string]string
		want        bool
	}{
		{
			name:        "annotation set to true",
			annotations: map[string]string{"test.annotation/export": "true"},
			want:        true,
		},
		{
			name:        "annotation set to false",
			annotations: map[string]string{"test.annotation/export": "false"},
			want:        false,
		},
		{
			name:        "annotation with other value",
			annotations: map[string]string{"test.annotation/export": "other"},
			want:        false,
		},
		{
			name:        "annotation missing",
			annotations: map[string]string{"other.annotation": "true"},
			want:        false,
		},
		{
			name:        "nil annotations",
			annotations: nil,
			want:        false,
		},
		{
			name:        "empty annotations map",
			annotations: map[string]string{},
			want:        false,
		},
		{
			name: "annotation true with other annotations",
			annotations: map[string]string{
				"test.annotation/export": "true",
				"other.annotation":       "value",
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := checkAnnotation(tt.annotations, annotation)
			assert.Equal(t, tt.want, result)
		})
	}
}
