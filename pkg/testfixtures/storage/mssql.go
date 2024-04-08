package storage

import (
	"context"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/cenkalti/backoff/v4"
	"github.com/docker/docker/api/types/container"
	_ "github.com/microsoft/go-mssqldb" // MSSQL Driver.
	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	testcontainersmssql "github.com/testcontainers/testcontainers-go/modules/mssql"

	"github.com/openfga/openfga/assets"
)

const (
	msSQLImage = "mcr.microsoft.com/mssql/server:2022-latest"
)

type msSQLTestContainer struct {
	addr     string
	version  int64
	username string
	password string
}

// NewMSSQLTestContainer returns an implementation of the DatastoreTestContainer interface
// for MSSQL.
func NewMSSQLTestContainer() *msSQLTestContainer {
	return &msSQLTestContainer{}
}

func (m *msSQLTestContainer) GetDatabaseSchemaVersion() int64 {
	return m.version
}

// RunMSSQLTestContainer runs a MSSQL container, connects to it, and returns a
// bootstrapped implementation of the DatastoreTestContainer interface wired up for the
// MSSQL datastore engine.
func (m *msSQLTestContainer) RunMSSQLTestContainer(t testing.TB) DatastoreTestContainer {
	ctx := context.Background()

	mssqlContainer, err := testcontainersmssql.RunContainer(ctx,
		testcontainers.WithImage(msSQLImage),
		testcontainers.WithHostConfigModifier(func(hostConfig *container.HostConfig) {
			hostConfig.Tmpfs = map[string]string{"/var/lib/mssql": ""}
		}),
		testcontainersmssql.WithAcceptEULA(),
		testcontainersmssql.WithPassword("pKC8mMA_qu5SLeaG"),
	)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, mssqlContainer.Terminate(ctx)) })

	mssqlHost, err := mssqlContainer.Host(ctx)
	require.NoError(t, err)

	mssqlPort, err := mssqlContainer.MappedPort(ctx, "1433/tcp")
	require.NoError(t, err)

	msSQLTestContainer := &msSQLTestContainer{
		addr:     net.JoinHostPort(mssqlHost, mssqlPort.Port()),
		username: "sa",
		password: "pKC8mMA_qu5SLeaG",
	}
	uri := fmt.Sprintf("sqlserver://%s:%s@%s", msSQLTestContainer.username, msSQLTestContainer.password, msSQLTestContainer.addr)

	goose.SetLogger(goose.NopLogger())

	db, err := goose.OpenDBWithDriver("sqlserver", uri)
	require.NoError(t, err)
	defer db.Close()

	backoffPolicy := backoff.NewExponentialBackOff()
	backoffPolicy.MaxElapsedTime = 2 * time.Minute
	err = backoff.Retry(
		func() error {
			return db.Ping()
		},
		backoffPolicy,
	)
	if err != nil {
		t.Fatalf("failed to connect to mssql container: %v", err)
	}

	goose.SetBaseFS(assets.EmbedMigrations)

	err = goose.Up(db, assets.MSSQLMigrationDir)
	require.NoError(t, err)
	version, err := goose.GetDBVersion(db)
	require.NoError(t, err)
	msSQLTestContainer.version = version

	return msSQLTestContainer
}

// GetConnectionURI returns the mssql connection uri for the running mssql test container.
func (m *msSQLTestContainer) GetConnectionURI(includeCredentials bool) string {
	creds := ""
	if includeCredentials {
		creds = fmt.Sprintf("%s:%s@", m.username, m.password)
	}

	return fmt.Sprintf(
		"sqlserver://%s%s",
		creds,
		m.addr,
	)
}

func (m *msSQLTestContainer) GetUsername() string {
	return m.username
}

func (m *msSQLTestContainer) GetPassword() string {
	return m.password
}
