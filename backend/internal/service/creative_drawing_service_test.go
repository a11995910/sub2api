package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCreativeDrawingTaskTimeoutAllowsImageGatewayRetries(t *testing.T) {
	require.Equal(t, 30*time.Minute, creativeDrawingTaskTimeout)
}

func TestNormalizeCreativeDrawingTaskErrorMapsDeadlineForEdit(t *testing.T) {
	got := normalizeCreativeDrawingTaskError(context.DeadlineExceeded, &CreativeDrawingTask{Mode: CreativeDrawingModeEdit})

	require.Contains(t, got, "参考图作画超时")
	require.NotContains(t, got, "context deadline exceeded")
}

func TestNormalizeCreativeDrawingTaskErrorMapsDeadlineForGenerate(t *testing.T) {
	got := normalizeCreativeDrawingTaskError(errors.New("creative drawing gateway request failed: context deadline exceeded"), &CreativeDrawingTask{Mode: CreativeDrawingModeGenerate})

	require.Equal(t, "图片生成超时，请重试", got)
}

func TestNormalizeCreativeDrawingTaskErrorKeepsGatewayMessage(t *testing.T) {
	got := normalizeCreativeDrawingTaskError(errors.New("上游图片接口返回 400"), &CreativeDrawingTask{Mode: CreativeDrawingModeEdit})

	require.Equal(t, "上游图片接口返回 400", got)
}
