package service

import (
	"context"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
)

var ErrPelicanShowcaseItemNotFound = infraerrors.NotFound("PELICAN_SHOWCASE_ITEM_NOT_FOUND", "showcase item not found")

type PelicanShowcaseItem struct {
	ID              int64     `json:"id"`
	GroupID         int64     `json:"group_id"`
	ModelID         string    `json:"model_id"`
	ReasoningEffort string    `json:"reasoning_effort"`
	LatencyMs       int64     `json:"latency_ms"`
	GeneratedAt     time.Time `json:"generated_at"`
	ResponseText    string    `json:"response_text,omitempty"`
}

type PelicanShowcaseGroup struct {
	ID       int64                  `json:"id"`
	Name     string                 `json:"name"`
	Platform string                 `json:"platform"`
	Items    []*PelicanShowcaseItem `json:"items"`
}

type PelicanShowcaseView struct {
	Enabled       bool                    `json:"enabled"`
	APIEnabled    bool                    `json:"api_enabled"`
	MaxItems      int                     `json:"max_items"`
	RetentionDays int                     `json:"retention_days"`
	Groups        []*PelicanShowcaseGroup `json:"groups"`
}

type PelicanShowcaseSnapshot struct {
	GroupID         int64
	SourceResultID  int64
	ModelID         string
	ReasoningEffort string
	ResponseText    string
	LatencyMs       int64
	GeneratedAt     time.Time
}

type PelicanShowcaseRepository interface {
	Publish(ctx context.Context, snapshot PelicanShowcaseSnapshot, maxItems int) error
	Prune(ctx context.Context, maxItems int, before time.Time) error
	ListGroups(ctx context.Context) ([]*PelicanShowcaseGroup, error)
	ListItems(ctx context.Context, groupIDs []int64, maxItems int, since time.Time) ([]*PelicanShowcaseItem, error)
	GetItem(ctx context.Context, id int64, maxItems int, since time.Time) (*PelicanShowcaseItem, error)
	Delete(ctx context.Context, id int64) (bool, error)
}

type pelicanShowcaseSettings interface {
	GetPelicanShowcaseRuntime(ctx context.Context) (PelicanShowcaseRuntime, error)
	UpdatePelicanShowcaseSettings(ctx context.Context, enabled bool, cfg PelicanShowcaseConfig, apiEnabled *bool) (PelicanShowcaseRuntime, error)
}

type PelicanShowcaseService struct {
	repo     PelicanShowcaseRepository
	settings pelicanShowcaseSettings
}

func NewPelicanShowcaseService(repo PelicanShowcaseRepository, settingService *SettingService) *PelicanShowcaseService {
	return &PelicanShowcaseService{repo: repo, settings: settingService}
}

func (s *PelicanShowcaseService) PublishGroupResult(ctx context.Context, groupID int64, result *PelicanGroupTestResult) {
	if s == nil || result == nil || result.ID <= 0 || groupID <= 0 || result.Status != "success" {
		return
	}
	if !pelicanHTMLPattern.MatchString(result.ResponseText) {
		return
	}
	runtime, err := s.settings.GetPelicanShowcaseRuntime(ctx)
	if err != nil {
		logger.LegacyPrintf("service.pelican_showcase", "publish result=%d skipped: %v", result.ID, err)
		return
	}
	snapshot := PelicanShowcaseSnapshot{
		GroupID:        groupID,
		SourceResultID: result.ID,
		ResponseText:   result.ResponseText,
		LatencyMs:      result.LatencyMs,
		GeneratedAt:    result.StartedAt,
	}
	if cfg := result.PelicanConfig; cfg != nil {
		snapshot.ModelID, snapshot.ReasoningEffort = cfg.ModelID, cfg.ReasoningEffort
	}
	if snapshot.GeneratedAt.IsZero() {
		snapshot.GeneratedAt = result.CreatedAt
	}
	if err := s.repo.Publish(ctx, snapshot, runtime.Config.MaxItems); err != nil {
		logger.LegacyPrintf("service.pelican_showcase", "publish result=%d failed: %v", result.ID, err)
	}
}

func (s *PelicanShowcaseService) Settings(ctx context.Context) (PelicanShowcaseRuntime, error) {
	return s.settings.GetPelicanShowcaseRuntime(ctx)
}

func (s *PelicanShowcaseService) UpdateSettings(ctx context.Context, enabled bool, cfg PelicanShowcaseConfig, apiEnabled *bool) (PelicanShowcaseRuntime, error) {
	return s.settings.UpdatePelicanShowcaseSettings(ctx, enabled, cfg, apiEnabled)
}

func (s *PelicanShowcaseService) Cleanup(ctx context.Context, now time.Time) {
	if s == nil {
		return
	}
	runtime, err := s.settings.GetPelicanShowcaseRuntime(ctx)
	if err != nil {
		logger.LegacyPrintf("service.pelican_showcase", "cleanup skipped: %v", err)
		return
	}
	cfg := runtime.Config
	if err := s.repo.Prune(ctx, cfg.MaxItems, cfg.retentionCutoff(now)); err != nil {
		logger.LegacyPrintf("service.pelican_showcase", "cleanup failed: %v", err)
	}
}

func (s *PelicanShowcaseService) View(ctx context.Context, now time.Time) (*PelicanShowcaseView, error) {
	runtime, err := s.settings.GetPelicanShowcaseRuntime(ctx)
	if err != nil {
		return nil, err
	}
	cfg := runtime.Config
	view := &PelicanShowcaseView{Enabled: runtime.Enabled, APIEnabled: runtime.Enabled && runtime.APIEnabled,
		MaxItems: cfg.MaxItems, Groups: []*PelicanShowcaseGroup{}}
	if cfg.AutoCleanup {
		view.RetentionDays = cfg.RetentionDays
	}
	if !runtime.Enabled {
		return view, nil
	}
	groups, err := s.repo.ListGroups(ctx)
	if err != nil {
		return nil, err
	}
	if len(groups) == 0 {
		return view, nil
	}
	groupIDs := make([]int64, 0, len(groups))
	byID := make(map[int64]*PelicanShowcaseGroup, len(groups))
	for _, group := range groups {
		group.Items = []*PelicanShowcaseItem{}
		groupIDs = append(groupIDs, group.ID)
		byID[group.ID] = group
	}
	items, err := s.repo.ListItems(ctx, groupIDs, cfg.MaxItems, cfg.retentionCutoff(now))
	if err != nil {
		return nil, err
	}
	for _, item := range items {
		if group := byID[item.GroupID]; group != nil {
			group.Items = append(group.Items, item)
		}
	}
	view.Groups = groups
	return view, nil
}

func (s *PelicanShowcaseService) Item(ctx context.Context, id int64, now time.Time) (*PelicanShowcaseItem, error) {
	runtime, err := s.settings.GetPelicanShowcaseRuntime(ctx)
	if err != nil {
		return nil, err
	}
	cfg := runtime.Config
	if !runtime.Enabled || id <= 0 {
		return nil, ErrPelicanShowcaseItemNotFound
	}
	item, err := s.repo.GetItem(ctx, id, cfg.MaxItems, cfg.retentionCutoff(now))
	if err != nil {
		return nil, err
	}
	if item == nil {
		return nil, ErrPelicanShowcaseItemNotFound
	}
	return item, nil
}

func (s *PelicanShowcaseService) Remove(ctx context.Context, id int64) error {
	deleted, err := s.repo.Delete(ctx, id)
	if err != nil {
		return err
	}
	if !deleted {
		return ErrPelicanShowcaseItemNotFound
	}
	return nil
}
