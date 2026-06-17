//go:build unit

package service

import (
	"context"
	"database/sql"
	"testing"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/enttest"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/stretchr/testify/require"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	_ "modernc.org/sqlite"
)

func TestAffiliateGroupAccessRewardClaimedOncePerInviterAndInvitee(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	client := newPaymentAffiliateRewardTestClient(t, "payment_reward_claim")

	inviterID := int64(23)
	inviteeID := int64(351)
	groupID := int64(31)
	first, err := tryClaimAffiliateGroupAccessRewardForInvitee(ctx, client, inviterID, inviteeID, groupID, map[string]any{
		"source_type":     "payment_order",
		"source_order_id": int64(1001),
		"order_type":      payment.OrderTypeBalance,
		"validity_days":   5,
	})
	require.NoError(t, err)
	require.True(t, first)

	second, err := tryClaimAffiliateGroupAccessRewardForInvitee(ctx, client, inviterID, inviteeID, groupID, map[string]any{
		"source_type":     "payment_order",
		"source_order_id": int64(1002),
		"order_type":      payment.OrderTypeBalance,
		"validity_days":   5,
	})
	require.NoError(t, err)
	require.False(t, second)

	otherGroup, err := tryClaimAffiliateGroupAccessRewardForInvitee(ctx, client, inviterID, inviteeID, groupID+1, map[string]any{
		"source_type":     "payment_order",
		"source_order_id": int64(1003),
		"order_type":      payment.OrderTypeBalance,
		"validity_days":   5,
	})
	require.NoError(t, err)
	require.False(t, otherGroup)

	otherInvitee, err := tryClaimAffiliateGroupAccessRewardForInvitee(ctx, client, inviterID, inviteeID+1, groupID, map[string]any{
		"source_type":     "payment_order",
		"source_order_id": int64(1004),
		"order_type":      payment.OrderTypeBalance,
		"validity_days":   5,
	})
	require.NoError(t, err)
	require.True(t, otherInvitee)

	count, err := client.PaymentAuditLog.Query().Count(ctx)
	require.NoError(t, err)
	require.Equal(t, 2, count)
}

func newPaymentAffiliateRewardTestClient(t *testing.T, name string) *dbent.Client {
	t.Helper()

	db, err := sql.Open("sqlite", "file:"+name+"?mode=memory&cache=shared&_fk=1")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	_, err = db.Exec("PRAGMA foreign_keys = ON")
	require.NoError(t, err)

	drv := entsql.OpenDB(dialect.SQLite, db)
	client := enttest.NewClient(t, enttest.WithOptions(dbent.Driver(drv)))
	t.Cleanup(func() { _ = client.Close() })
	return client
}
