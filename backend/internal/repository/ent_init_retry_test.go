package repository

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"net"
	"syscall"
	"testing"
	"time"

	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

func transientDatabaseError(code string) error {
	return &pq.Error{Code: pq.ErrorCode(code), Message: "database is temporarily unavailable"}
}

func TestIsTransientDatabaseInitializationError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "postgres is starting", err: transientDatabaseError("57P03"), want: true},
		{name: "connection failure", err: fmt.Errorf("wrapped: %w", transientDatabaseError("08006")), want: true},
		{name: "tcp connection refused", err: fmt.Errorf("wrapped: %w", &net.OpError{Op: "dial", Net: "tcp", Err: syscall.ECONNREFUSED}), want: true},
		{name: "tcp connection reset", err: fmt.Errorf("wrapped: %w", &net.OpError{Op: "read", Net: "tcp", Err: syscall.ECONNRESET}), want: true},
		{name: "broken pipe", err: fmt.Errorf("wrapped: %w", &net.OpError{Op: "write", Net: "tcp", Err: syscall.EPIPE}), want: true},
		{name: "bad pooled connection", err: fmt.Errorf("wrapped: %w", driver.ErrBadConn), want: true},
		{name: "eof during startup", err: fmt.Errorf("wrapped: %w", io.EOF), want: true},
		{name: "unexpected eof during startup", err: fmt.Errorf("wrapped: %w", io.ErrUnexpectedEOF), want: true},
		{name: "temporary dns failure", err: &net.DNSError{Err: "temporary failure", Name: "postgres", IsTemporary: true}, want: true},
		{name: "authentication failure", err: transientDatabaseError("28P01"), want: false},
		{name: "database does not exist", err: transientDatabaseError("3D000"), want: false},
		{name: "sql syntax error", err: transientDatabaseError("42601"), want: false},
		{name: "permanent dns failure", err: &net.DNSError{Err: "no such host", Name: "bad-host", IsNotFound: true}, want: false},
		{name: "context canceled", err: context.Canceled, want: false},
		{name: "context deadline", err: context.DeadlineExceeded, want: false},
		{name: "connection text without typed cause", err: errors.New("dial tcp: connection refused"), want: false},
		{name: "migration error", err: errors.New("migration checksum mismatch"), want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, isTransientDatabaseInitializationError(tt.err))
		})
	}
}

func TestInitializeDatabaseWithRetryEventuallySucceeds(t *testing.T) {
	attempts := 0
	var delays []time.Duration
	err := initializeDatabaseWithRetryWithWait(context.Background(), func(context.Context) error {
		attempts++
		if attempts <= 3 {
			return transientDatabaseError("57P03")
		}
		return nil
	}, func(_ context.Context, delay time.Duration) error {
		delays = append(delays, delay)
		return nil
	})

	require.NoError(t, err)
	require.Equal(t, 4, attempts)
	require.Equal(t, []time.Duration{time.Second, 2 * time.Second, 4 * time.Second}, delays)
}

func TestInitializeDatabaseWithRetryRetriesTransientNetworkError(t *testing.T) {
	attempts := 0
	var delays []time.Duration
	err := initializeDatabaseWithRetryWithWait(context.Background(), func(context.Context) error {
		attempts++
		if attempts == 1 {
			return fmt.Errorf("open postgres connection: %w", &net.OpError{Op: "dial", Net: "tcp", Err: syscall.ECONNREFUSED})
		}
		return nil
	}, func(_ context.Context, delay time.Duration) error {
		delays = append(delays, delay)
		return nil
	})

	require.NoError(t, err)
	require.Equal(t, 2, attempts)
	require.Equal(t, []time.Duration{time.Second}, delays)
}

func TestInitializeDatabaseWithRetryFailsFastForPermanentError(t *testing.T) {
	attempts := 0
	waitCalled := false
	permanentErr := transientDatabaseError("28P01")
	err := initializeDatabaseWithRetryWithWait(context.Background(), func(context.Context) error {
		attempts++
		return permanentErr
	}, func(_ context.Context, _ time.Duration) error {
		waitCalled = true
		return nil
	})

	require.ErrorIs(t, err, permanentErr)
	require.Equal(t, 1, attempts)
	require.False(t, waitCalled)
}

func TestInitializeDatabaseWithRetryStopsWhenContextIsCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	attempts := 0
	err := initializeDatabaseWithRetryWithWait(ctx, func(context.Context) error {
		attempts++
		return transientDatabaseError("57P03")
	}, func(ctx context.Context, _ time.Duration) error {
		cancel()
		return ctx.Err()
	})

	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, 1, attempts)
}

func TestInitializeDatabaseWithRetryReturnsLastErrorAfterLimit(t *testing.T) {
	attempts := 0
	lastErr := transientDatabaseError("08006")
	var delays []time.Duration
	err := initializeDatabaseWithRetryWithWait(context.Background(), func(context.Context) error {
		attempts++
		return lastErr
	}, func(_ context.Context, delay time.Duration) error {
		delays = append(delays, delay)
		return nil
	})

	require.ErrorIs(t, err, lastErr)
	require.Equal(t, maxDatabaseInitializationRetries+1, attempts)
	require.Len(t, delays, maxDatabaseInitializationRetries)
	require.Equal(t, databaseInitializationRetryMax, delays[len(delays)-1])
}

func TestInitializeDatabaseWithRetryAllowsIdempotentMigrationRetry(t *testing.T) {
	applied := make(map[string]bool)
	attempts := 0
	err := initializeDatabaseWithRetryWithWait(context.Background(), func(context.Context) error {
		attempts++
		if !applied["001_init.sql"] {
			applied["001_init.sql"] = true
			return transientDatabaseError("57P03")
		}
		return nil
	}, func(_ context.Context, _ time.Duration) error { return nil })

	require.NoError(t, err)
	require.Equal(t, 2, attempts)
	require.True(t, applied["001_init.sql"])
}
