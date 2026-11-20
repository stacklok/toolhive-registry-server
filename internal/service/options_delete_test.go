package service

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDeleteVersionOptions(t *testing.T) {
	t.Parallel()

	server := &DeleteServerVersionOptions{}
	require.NoError(t, WithName("io.test/srv")(server))
	require.NoError(t, WithVersion("1.0.0")(server))
	require.NoError(t, WithJWTClaims(map[string]any{"org": "acme"})(server))
	assert.Equal(t, DeleteServerVersionOptions{
		ServerName: "io.test/srv", Version: "1.0.0", JWTClaims: map[string]any{"org": "acme"},
	}, *server)

	skill := &DeleteSkillVersionOptions{}
	require.NoError(t, WithName("skill")(skill))
	require.NoError(t, WithVersion("2.0.0")(skill))
	assert.Equal(t, "skill", skill.Name)
	assert.Equal(t, "2.0.0", skill.Version)

	plugin := &DeletePluginVersionOptions{}
	require.NoError(t, WithName("plugin")(plugin))
	require.NoError(t, WithVersion("3.0.0")(plugin))
	assert.Equal(t, "plugin", plugin.Name)
	assert.Equal(t, "3.0.0", plugin.Version)
}

func TestNameAndVersionOptionsRejectOtherTargets(t *testing.T) {
	t.Parallel()

	assert.Error(t, WithName("x")(&PublishServerVersionOptions{}))
	assert.Error(t, WithVersion("1")(&PublishServerVersionOptions{}))
}
