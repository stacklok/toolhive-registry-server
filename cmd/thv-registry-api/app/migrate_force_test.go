package app

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseForceVersion(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		arg     string
		want    int
		wantErr bool
	}{
		{arg: "24", want: 24},
		{arg: "0", want: 0},
		{arg: "-1", want: -1},
		{arg: "-2", wantErr: true},
		{arg: "latest", wantErr: true},
		{arg: "", wantErr: true},
	} {
		t.Run(tc.arg, func(t *testing.T) {
			t.Parallel()
			got, err := parseForceVersion(tc.arg)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestMigrateForceIsRegistered(t *testing.T) {
	t.Parallel()
	cmd, _, err := migrateCmd.Find([]string{"force"})
	require.NoError(t, err)
	require.Equal(t, migrateForceCmd, cmd)
	require.Error(t, cmd.Args(cmd, nil), "force needs a version")
}
