//go:build pelicanpostgres && !integration

package repository

import (
	"context"
	"database/sql"
	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"fmt"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	_ "github.com/Wei-Shaw/sub2api/ent/runtime"
	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
	"os"
	"testing"
)

// 仅连接显式指定的本地临时数据库，复用实际迁移和仓储测试，不依赖 Docker。
var integrationDB *sql.DB
var pelicanTestClient *dbent.Client

func TestMain(m *testing.M) {
	dsn := os.Getenv("PELICAN_TEST_POSTGRES_DSN")
	if dsn == "" {
		fmt.Fprintln(os.Stderr, "请指定临时数据库 PELICAN_TEST_POSTGRES_DSN")
		os.Exit(1)
	}
	if err := timezone.Init("UTC"); err != nil {
		panic(err)
	}
	var err error
	integrationDB, err = sql.Open("postgres", dsn)
	if err != nil {
		panic(err)
	}
	if err = ApplyMigrations(context.Background(), integrationDB); err != nil {
		panic(err)
	}
	pelicanTestClient = dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, integrationDB)))
	code := m.Run()
	_ = pelicanTestClient.Close()
	_ = integrationDB.Close()
	os.Exit(code)
}
func testEntClient(t *testing.T) *dbent.Client { t.Helper(); return pelicanTestClient }
func mustCreateGroup(t *testing.T, client *dbent.Client, group *service.Group) *service.Group {
	t.Helper()
	if group.Status == "" {
		group.Status = service.StatusActive
	}
	if group.Platform == "" {
		group.Platform = service.PlatformAnthropic
	}
	err := NewGroupRepository(client, integrationDB).Create(context.Background(), group)
	require.NoError(t, err)
	return group
}
