package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	apptimezone "github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

type lotteryRepository struct {
	db *sql.DB
}

func NewLotteryRepository(db *sql.DB) service.LotteryRepository {
	return &lotteryRepository{db: db}
}

func (r *lotteryRepository) GetConfig(ctx context.Context) (service.LotteryConfigSnapshot, error) {
	var (
		snapshot service.LotteryConfigSnapshot
		prizes   []byte
	)
	err := r.db.QueryRowContext(ctx, `
		SELECT enabled, usage_threshold_tokens, award_mode, prizes, version, updated_at
		FROM sub2api_lottery_settings
		WHERE id = 1
	`).Scan(
		&snapshot.Config.Enabled,
		&snapshot.Config.UsageThresholdTokens,
		&snapshot.Config.AwardMode,
		&prizes,
		&snapshot.Version,
		&snapshot.UpdatedAt,
	)
	if err != nil {
		return service.LotteryConfigSnapshot{}, err
	}
	if err := json.Unmarshal(prizes, &snapshot.Config.Prizes); err != nil {
		return service.LotteryConfigSnapshot{}, fmt.Errorf("解析抽奖奖品配置: %w", err)
	}
	normalized, err := service.NormalizeLotteryConfig(snapshot.Config)
	if err != nil {
		return service.LotteryConfigSnapshot{}, fmt.Errorf("抽奖配置无效: %w", err)
	}
	snapshot.Config = normalized
	return snapshot, nil
}

func (r *lotteryRepository) SaveConfig(ctx context.Context, config service.LotteryConfig, updatedBy int64) (service.LotteryConfigSnapshot, error) {
	normalized, err := service.NormalizeLotteryConfig(config)
	if err != nil {
		return service.LotteryConfigSnapshot{}, err
	}
	prizes, err := json.Marshal(normalized.Prizes)
	if err != nil {
		return service.LotteryConfigSnapshot{}, err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return service.LotteryConfigSnapshot{}, err
	}
	defer func() { _ = tx.Rollback() }()

	var snapshot service.LotteryConfigSnapshot
	err = tx.QueryRowContext(ctx, `
		UPDATE sub2api_lottery_settings
		SET usage_threshold_tokens = $1,
		    award_mode = $2,
		    prizes = $3,
		    version = version + 1,
		    updated_by = $4,
		    updated_at = NOW()
		WHERE id = 1
		RETURNING enabled, version, updated_at
	`, normalized.UsageThresholdTokens, normalized.AwardMode, prizes, updatedBy).Scan(
		&normalized.Enabled,
		&snapshot.Version,
		&snapshot.UpdatedAt,
	)
	if err != nil {
		return service.LotteryConfigSnapshot{}, err
	}
	effectiveDate := time.Now().In(apptimezone.Location()).Format("2006-01-02")
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO sub2api_lottery_rule_versions (
			effective_date, enabled, usage_threshold_tokens, award_mode, created_by
		) VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (effective_date) DO UPDATE SET
			enabled = EXCLUDED.enabled,
			usage_threshold_tokens = EXCLUDED.usage_threshold_tokens,
			award_mode = EXCLUDED.award_mode,
			created_by = EXCLUDED.created_by,
			updated_at = NOW()
	`, effectiveDate, normalized.Enabled, normalized.UsageThresholdTokens, normalized.AwardMode, updatedBy); err != nil {
		return service.LotteryConfigSnapshot{}, err
	}
	if err := tx.Commit(); err != nil {
		return service.LotteryConfigSnapshot{}, err
	}
	snapshot.Config = normalized
	return snapshot, nil
}

func (r *lotteryRepository) ReconcileUserChances(ctx context.Context, userID int64) error {
	tzName := apptimezone.Location().String()
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO sub2api_lottery_user_states (user_id)
		VALUES ($1)
		ON CONFLICT (user_id) DO NOTHING
	`, userID); err != nil {
		return err
	}
	var lockedUserID int64
	if err := tx.QueryRowContext(ctx, `
		SELECT user_id
		FROM sub2api_lottery_user_states
		WHERE user_id = $1
		FOR UPDATE
	`, userID).Scan(&lockedUserID); err != nil {
		return err
	}

	var earned int64
	err = tx.QueryRowContext(ctx, `
		WITH rule_start AS (
			SELECT MIN(effective_date) AS first_date
			FROM sub2api_lottery_rule_versions
		), daily_usage AS (
			SELECT
				(created_at AT TIME ZONE $2)::date AS usage_date,
				COALESCE(SUM(
					input_tokens::bigint + output_tokens::bigint +
					cache_creation_tokens::bigint + cache_read_tokens::bigint
				), 0)::bigint AS usage_tokens
			FROM usage_logs
			WHERE user_id = $1
			  AND created_at >= COALESCE(
				((SELECT first_date FROM rule_start)::timestamp AT TIME ZONE $2),
				NOW()
			  )
			GROUP BY 1
		), qualified AS (
			SELECT
				daily_usage.usage_date,
				daily_usage.usage_tokens,
				rule.usage_threshold_tokens,
				rule.award_mode,
				CASE
					WHEN rule.award_mode = 'per_threshold'
						THEN daily_usage.usage_tokens / rule.usage_threshold_tokens
					ELSE 1
				END::bigint AS earned_chances
			FROM daily_usage
			JOIN LATERAL (
				SELECT enabled, usage_threshold_tokens, award_mode
				FROM sub2api_lottery_rule_versions
				WHERE effective_date <= daily_usage.usage_date
				ORDER BY effective_date DESC
				LIMIT 1
			) AS rule ON rule.enabled
			WHERE daily_usage.usage_tokens >= rule.usage_threshold_tokens
		), candidates AS (
			SELECT
				qualified.*,
				COALESCE(daily_award.awarded_chances, 0)::bigint AS previous_chances
			FROM qualified
			LEFT JOIN sub2api_lottery_daily_awards AS daily_award
				ON daily_award.user_id = $1
				AND daily_award.usage_date = qualified.usage_date
		), upserted AS (
			INSERT INTO sub2api_lottery_daily_awards (
				user_id, usage_date, usage_tokens, threshold_tokens, award_mode, awarded_chances
			)
			SELECT
				$1, usage_date, usage_tokens, usage_threshold_tokens, award_mode, earned_chances
			FROM candidates
			ON CONFLICT (user_id, usage_date) DO UPDATE SET
				usage_tokens = EXCLUDED.usage_tokens,
				threshold_tokens = EXCLUDED.threshold_tokens,
				award_mode = EXCLUDED.award_mode,
				awarded_chances = EXCLUDED.awarded_chances,
				awarded_at = NOW()
			WHERE sub2api_lottery_daily_awards.awarded_chances < EXCLUDED.awarded_chances
			RETURNING usage_date
		)
		SELECT COALESCE(SUM(candidates.earned_chances - candidates.previous_chances), 0)::bigint
		FROM candidates
		JOIN upserted USING (usage_date)
	`, userID, tzName).Scan(&earned)
	if err != nil {
		return err
	}
	if earned > 0 {
		if _, err := tx.ExecContext(ctx, `
			UPDATE sub2api_lottery_user_states
			SET available_chances = available_chances + $2,
			    total_earned = total_earned + $2,
			    updated_at = NOW()
			WHERE user_id = $1
		`, userID, earned); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (r *lotteryRepository) GetUserState(ctx context.Context, userID int64) (service.LotteryUserState, error) {
	location := apptimezone.Location()
	now := time.Now().In(location)
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, location)
	end := start.AddDate(0, 0, 1)
	date := start.Format("2006-01-02")
	var state service.LotteryUserState
	err := r.db.QueryRowContext(ctx, `
		SELECT
			COALESCE(user_state.available_chances, 0),
			COALESCE(user_state.total_earned, 0),
			COALESCE(user_state.total_drawn, 0),
			COALESCE((
				SELECT SUM(
					input_tokens::bigint + output_tokens::bigint +
					cache_creation_tokens::bigint + cache_read_tokens::bigint
				)::bigint
				FROM usage_logs
				WHERE user_id = $1 AND created_at >= $2 AND created_at < $3
			), 0),
			COALESCE(rule.usage_threshold_tokens, settings.usage_threshold_tokens),
			COALESCE(rule.award_mode, settings.award_mode),
			COALESCE(daily_award.awarded_chances, 0),
			COALESCE(daily_award.awarded_chances, 0) > 0
		FROM sub2api_lottery_settings AS settings
		LEFT JOIN sub2api_lottery_user_states AS user_state ON user_state.user_id = $1
		LEFT JOIN LATERAL (
			SELECT usage_threshold_tokens, award_mode
			FROM sub2api_lottery_rule_versions
			WHERE effective_date <= $4::date
			ORDER BY effective_date DESC
			LIMIT 1
		) AS rule ON TRUE
		LEFT JOIN sub2api_lottery_daily_awards AS daily_award
			ON daily_award.user_id = $1 AND daily_award.usage_date = $4::date
		WHERE settings.id = 1
	`, userID, start.UTC(), end.UTC(), date).Scan(
		&state.AvailableChances,
		&state.TotalEarned,
		&state.TotalDrawn,
		&state.TodayUsageTokens,
		&state.TodayThreshold,
		&state.TodayAwardMode,
		&state.TodayAwardedChances,
		&state.TodayQualified,
	)
	return state, err
}

func (r *lotteryRepository) CommitDraw(
	ctx context.Context,
	userID int64,
	selection service.LotterySelection,
	configVersion int64,
) (service.LotteryDraw, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return service.LotteryDraw{}, err
	}
	defer func() { _ = tx.Rollback() }()

	var chances int64
	err = tx.QueryRowContext(ctx, `
		SELECT available_chances
		FROM sub2api_lottery_user_states
		WHERE user_id = $1
		FOR UPDATE
	`, userID).Scan(&chances)
	if errors.Is(err, sql.ErrNoRows) || chances <= 0 {
		return service.LotteryDraw{}, service.ErrLotteryNoChance
	}
	if err != nil {
		return service.LotteryDraw{}, err
	}

	prizeID := service.LotteryThanksPrizeID
	prizeName := "谢谢惠顾"
	rewardAmount := 0.0
	probability := selection.ThanksProbabilityBasis
	if selection.Prize != nil {
		prizeID = selection.Prize.ID
		prizeName = selection.Prize.Name
		rewardAmount = selection.Prize.RewardAmount
		probability = selection.Prize.ProbabilityBasisPoints
	}
	chanceAfter := chances - 1
	if _, err := tx.ExecContext(ctx, `
		UPDATE sub2api_lottery_user_states
		SET available_chances = $2,
		    total_drawn = total_drawn + 1,
		    updated_at = NOW()
		WHERE user_id = $1
	`, userID, chanceAfter); err != nil {
		return service.LotteryDraw{}, err
	}

	var balanceAfter float64
	err = tx.QueryRowContext(ctx, `
		UPDATE users
		SET balance = balance + $2, updated_at = NOW()
		WHERE id = $1 AND deleted_at IS NULL
		RETURNING balance
	`, userID, rewardAmount).Scan(&balanceAfter)
	if err != nil {
		return service.LotteryDraw{}, err
	}

	draw := service.LotteryDraw{
		UserID:                 userID,
		PrizeID:                prizeID,
		PrizeName:              prizeName,
		RewardAmount:           rewardAmount,
		ProbabilityBasisPoints: probability,
		RandomRoll:             selection.Roll,
		ConfigVersion:          configVersion,
		ChanceBefore:           chances,
		ChanceAfter:            chanceAfter,
		BalanceAfter:           balanceAfter,
	}
	err = tx.QueryRowContext(ctx, `
		INSERT INTO sub2api_lottery_draws (
			user_id, prize_id, prize_name, reward_amount,
			probability_basis_points, random_roll, config_version,
			chance_before, chance_after, balance_after
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		RETURNING id, created_at
	`,
		draw.UserID,
		draw.PrizeID,
		draw.PrizeName,
		draw.RewardAmount,
		draw.ProbabilityBasisPoints,
		draw.RandomRoll,
		draw.ConfigVersion,
		draw.ChanceBefore,
		draw.ChanceAfter,
		draw.BalanceAfter,
	).Scan(&draw.ID, &draw.CreatedAt)
	if err != nil {
		return service.LotteryDraw{}, err
	}
	if err := tx.Commit(); err != nil {
		return service.LotteryDraw{}, err
	}
	return draw, nil
}

func (r *lotteryRepository) ListUserDraws(ctx context.Context, userID int64, limit int) ([]service.LotteryDraw, error) {
	if limit <= 0 {
		return []service.LotteryDraw{}, nil
	}
	rows, err := r.db.QueryContext(ctx, lotteryDrawSelectSQL+`
		WHERE draw.user_id = $1
		ORDER BY draw.created_at DESC, draw.id DESC
		LIMIT $2
	`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanLotteryDraws(rows)
}

func (r *lotteryRepository) ListDraws(ctx context.Context, page, pageSize int) ([]service.LotteryDraw, int64, error) {
	var total int64
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sub2api_lottery_draws`).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := r.db.QueryContext(ctx, lotteryDrawSelectSQL+`
		ORDER BY draw.created_at DESC, draw.id DESC
		LIMIT $1 OFFSET $2
	`, pageSize, (page-1)*pageSize)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items, err := scanLotteryDraws(rows)
	return items, total, err
}

const lotteryDrawSelectSQL = `
	SELECT
		draw.id, draw.user_id, COALESCE(users.email, ''),
		draw.prize_id, draw.prize_name, draw.reward_amount,
		draw.probability_basis_points, draw.random_roll, draw.config_version,
		draw.chance_before, draw.chance_after, draw.balance_after, draw.created_at
	FROM sub2api_lottery_draws AS draw
	LEFT JOIN users ON users.id = draw.user_id
`

func scanLotteryDraws(rows *sql.Rows) ([]service.LotteryDraw, error) {
	items := make([]service.LotteryDraw, 0)
	for rows.Next() {
		var item service.LotteryDraw
		if err := rows.Scan(
			&item.ID,
			&item.UserID,
			&item.UserEmail,
			&item.PrizeID,
			&item.PrizeName,
			&item.RewardAmount,
			&item.ProbabilityBasisPoints,
			&item.RandomRoll,
			&item.ConfigVersion,
			&item.ChanceBefore,
			&item.ChanceAfter,
			&item.BalanceAfter,
			&item.CreatedAt,
		); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

var _ service.LotteryRepository = (*lotteryRepository)(nil)
