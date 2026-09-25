package database

import (
	"net"
	"strconv"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSetupTestDBContainer(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	first, cleanupFirst := SetupTestDBContainer(t, ctx)

	var (
		databaseName  string
		user          string
		serverVersion string
		fsync         string
		logStatements string
	)
	err := first.QueryRow(ctx, `
		SELECT current_database(), current_user,
		       current_setting('server_version_num'),
		       current_setting('fsync'),
		       current_setting('log_statement')
	`).Scan(&databaseName, &user, &serverVersion, &fsync, &logStatements)
	require.NoError(t, err)
	assert.Equal(t, DBName, databaseName)
	assert.Equal(t, DBUser, user)
	assert.Regexp(t, `^16\d{4}$`, serverVersion)
	assert.Equal(t, "off", fsync)
	assert.Equal(t, "all", logStatements)

	wrongPasswordConfig := first.Config().Copy()
	wrongPasswordConfig.Password = "wrong-password"
	wrongPasswordDB, err := pgx.ConnectConfig(ctx, wrongPasswordConfig)
	if wrongPasswordDB != nil {
		_ = wrongPasswordDB.Close(ctx)
	}
	require.Error(t, err)

	second, cleanupSecond := SetupTestDBContainer(t, ctx)
	require.NotEqual(t, first.Config().Port, second.Config().Port)

	_, err = first.Exec(ctx, "CREATE TABLE instance_one_only (id integer PRIMARY KEY)")
	require.NoError(t, err)
	var isolated bool
	err = second.QueryRow(ctx, "SELECT to_regclass('public.instance_one_only') IS NULL").Scan(&isolated)
	require.NoError(t, err)
	assert.True(t, isolated)

	firstPort := first.Config().Port
	cleanupFirst()
	cleanupFirst()
	listener, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(int(firstPort))))
	require.NoError(t, err)
	require.NoError(t, listener.Close())

	cleanupSecond()
	cleanupSecond()
}

func TestSetupTestDB(t *testing.T) {
	t.Parallel()

	first, cleanupFirst := SetupTestDB(t)
	second, cleanupSecond := SetupTestDB(t)

	assert.Equal(t, first.Config().Port, second.Config().Port)
	assert.NotEqual(t, first.Config().Database, second.Config().Database)

	var migrationsTableExists bool
	err := first.QueryRow(t.Context(), "SELECT to_regclass('public.schema_migrations') IS NOT NULL").
		Scan(&migrationsTableExists)
	require.NoError(t, err)
	assert.True(t, migrationsTableExists)

	_, codecRegistered := first.TypeMap().TypeForName("sync_status[]")
	assert.True(t, codecRegistered)

	cleanupFirst()
	cleanupFirst()
	cleanupSecond()
	cleanupSecond()
}
