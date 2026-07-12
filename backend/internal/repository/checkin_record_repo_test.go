package repository

import (
	"testing"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/stretchr/testify/require"
)

func TestCheckinRecordEntityToServicePreservesConsecutiveCount(t *testing.T) {
	record := checkinRecordEntityToService(&dbent.CheckinRecord{
		ConsecutiveCount: 4,
	})

	require.Equal(t, 4, record.ConsecutiveCount)
}
