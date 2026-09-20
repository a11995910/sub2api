package service

import (
	"context"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

var ErrContentModerationLogNotFound = infraerrors.NotFound("CONTENT_MODERATION_LOG_NOT_FOUND", "内容审核记录不存在")

func (s *ContentModerationService) GetLog(ctx context.Context, id int64) (*ContentModerationLog, error) {
	if id <= 0 {
		return nil, infraerrors.BadRequest("INVALID_CONTENT_MODERATION_LOG_ID", "内容审核记录 ID 无效")
	}
	return s.repo.GetLog(ctx, id)
}
