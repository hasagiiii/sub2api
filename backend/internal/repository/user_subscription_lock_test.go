package repository

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	_ "github.com/Wei-Shaw/sub2api/ent/runtime"
	"github.com/Wei-Shaw/sub2api/ent/usersubscription"
	"github.com/stretchr/testify/require"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
)

// recordAllQueriesMatcher 记录执行过的每条 SQL。
// 读取订阅现在会附带一次覆盖分组的补齐查询，单值捕获会被后一条覆盖。
type recordAllQueriesMatcher struct {
	queries *[]string
}

func (m recordAllQueriesMatcher) Match(_, actual string) error {
	*m.queries = append(*m.queries, actual)
	return nil
}

func TestUserSubscriptionGetByIDForUpdateLocksRow(t *testing.T) {
	var executed []string
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(recordAllQueriesMatcher{queries: &executed}))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	driver := entsql.OpenDB(dialect.Postgres, db)
	client := dbent.NewClient(dbent.Driver(driver))
	t.Cleanup(func() { _ = client.Close() })
	repo := NewUserSubscriptionRepository(client)
	now := time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC)

	// 列顺序随 usersubscription.Columns：plan_id 紧跟 group_id 之后。
	mock.ExpectQuery("locked subscription").WillReturnRows(
		sqlmock.NewRows(usersubscription.Columns).AddRow(
			int64(7), now, now, nil, int64(11), int64(13), nil, now, now.AddDate(0, 0, 30), "active",
			nil, nil, nil, 0.0, 0.0, 0.0, nil, now, "renewal",
		),
	)
	// 读取订阅后会补齐覆盖分组集合（额度池能用在哪些分组上）。
	// 批量补齐语句同时选出 subscription_id 与 group_id。
	mock.ExpectQuery("covered groups").WillReturnRows(
		sqlmock.NewRows([]string{"subscription_id", "group_id"}).AddRow(int64(7), int64(13)),
	)

	sub, err := repo.GetByIDForUpdate(context.Background(), 7)
	require.NoError(t, err)
	require.Equal(t, int64(7), sub.ID)
	require.Equal(t, []int64{13}, sub.GroupIDs, "covered groups must be hydrated")
	require.NoError(t, mock.ExpectationsWereMet())
	// 第一条是取订阅本身的语句，必须带行锁；后续的补齐查询会覆盖单值捕获，
	// 所以这里取记录里的第一条来断言。
	require.NotEmpty(t, executed)
	require.Contains(t, strings.ToUpper(normalizeSQLWhitespace(executed[0])), "FOR UPDATE")
}
