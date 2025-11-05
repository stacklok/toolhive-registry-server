package sources

import (
	"context"
	"crypto/sha256"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/stacklok/toolhive-registry-server/pkg/config"
)

const (
	// ConfigMapSourceDataKey is the default key used for registry data in ConfigMap sources
	ConfigMapSourceDataKey = "registry.json"
)

// ConfigMapSourceHandler handles registry data from Kubernetes ConfigMaps
type ConfigMapSourceHandler struct {
	client    client.Client
	validator SourceDataValidator
}

// NewConfigMapSourceHandler creates a new ConfigMap source handler
func NewConfigMapSourceHandler(k8sClient client.Client) *ConfigMapSourceHandler {
	return &ConfigMapSourceHandler{
		client:    k8sClient,
		validator: NewSourceDataValidator(),
	}
}

// Validate validates the ConfigMap source configuration
func (*ConfigMapSourceHandler) Validate(source *config.SourceConfig) error {
	if source.Type != config.SourceTypeConfigMap {
		return fmt.Errorf("invalid source type: expected %s, got %s",
			config.SourceTypeConfigMap, source.Type)
	}

	if source.ConfigMap == nil {
		return fmt.Errorf("configMap configuration is required for source type %s",
			config.SourceTypeConfigMap)
	}

	if source.ConfigMap.Name == "" {
		return fmt.Errorf("configMap name cannot be empty")
	}

	// Key defaults to ConfigMapSourceDataKey if not specified (handled by kubebuilder defaults)
	if source.ConfigMap.Key == "" {
		source.ConfigMap.Key = ConfigMapSourceDataKey
	}

	return nil
}

// fetchConfigMapData retrieves and validates ConfigMap data for the given config
func (h *ConfigMapSourceHandler) fetchConfigMapData(ctx context.Context, config *config.Config) (*string, error) {
	source := &config.Source

	// Validate source configuration
	if err := h.Validate(&config.Source); err != nil {
		return nil, fmt.Errorf("source validation failed: %w", err)
	}

	// Retrieve ConfigMap
	configMap := &corev1.ConfigMap{}
	configMapKey := types.NamespacedName{
		Name:      source.ConfigMap.Name,
		Namespace: source.ConfigMap.Namespace,
	}

	if err := h.client.Get(ctx, configMapKey, configMap); err != nil {
		return nil, fmt.Errorf("failed to get ConfigMap %s/%s: %w",
			source.ConfigMap.Namespace, source.ConfigMap.Name, err)
	}

	// Get registry data from ConfigMap
	key := source.ConfigMap.Key
	if key == "" {
		key = ConfigMapSourceDataKey
	}

	data, exists := configMap.Data[key]
	if !exists {
		return nil, fmt.Errorf("key %s not found in ConfigMap %s/%s",
			key, source.ConfigMap.Namespace, source.ConfigMap.Name)
	}

	return &data, nil
}

// FetchRegistry retrieves registry data from the ConfigMap source
func (h *ConfigMapSourceHandler) FetchRegistry(ctx context.Context, registryConfig *config.Config) (*FetchResult, error) {
	if registryConfig.Source.Format == config.SourceFormatUpstream {
		return nil, fmt.Errorf("upstream registry format is not yet supported")
	}

	// Fetch ConfigMap data using reusable function
	configMapData, err := h.fetchConfigMapData(ctx, registryConfig)
	if err != nil {
		return nil, err
	}

	// Convert string data to bytes
	registryData := []byte(*configMapData)

	// Validate and parse registry data
	reg, err := h.validator.ValidateData(registryData, registryConfig.Source.Format)
	if err != nil {
		return nil, fmt.Errorf("registry data validation failed: %w", err)
	}

	// Calculate hash using the same method as CurrentHash for consistency
	hash, err := h.CurrentHash(ctx, registryConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to calculate hash: %w", err)
	}

	// Create and return fetch result with pre-calculated hash
	return NewFetchResult(reg, hash, registryConfig.Source.Format), nil
}

// CurrentHash returns the current hash of the source data without performing a full fetch
func (h *ConfigMapSourceHandler) CurrentHash(ctx context.Context, registryConfig *config.Config) (string, error) {
	// Fetch ConfigMap data using reusable function
	configMapData, err := h.fetchConfigMapData(ctx, registryConfig)
	if err != nil {
		return "", err
	}

	// Compute and return hash of the data
	hash := fmt.Sprintf("%x", sha256.Sum256([]byte(*configMapData)))
	return hash, nil
}
