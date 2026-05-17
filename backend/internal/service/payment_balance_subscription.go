package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	dbuser "github.com/Wei-Shaw/sub2api/ent/user"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

// PurchaseSubscriptionWithBalanceResult 是用户使用灵石兑换订阅后的结果。
type PurchaseSubscriptionWithBalanceResult struct {
	PlanID       int64             `json:"plan_id"`
	Price        float64           `json:"price"`
	NewBalance   float64           `json:"new_balance"`
	RedeemCode   *RedeemCode       `json:"redeem_code"`
	Subscription *UserSubscription `json:"subscription,omitempty"`
}

// PurchaseSubscriptionWithBalance 使用用户余额购买订阅套餐，并自动生成/兑换一张订阅兑换码。
func (s *PaymentService) PurchaseSubscriptionWithBalance(ctx context.Context, userID, planID int64) (*PurchaseSubscriptionWithBalanceResult, error) {
	if userID <= 0 {
		return nil, infraerrors.Unauthorized("USER_NOT_AUTHENTICATED", "user not authenticated")
	}
	if s.redeemService == nil || s.subscriptionSvc == nil {
		return nil, fmt.Errorf("subscription purchase dependencies are not configured")
	}

	plan, err := s.validateSubOrder(ctx, CreateOrderRequest{
		PlanID: planID,
	})
	if err != nil {
		return nil, err
	}
	if plan.Price < 0 {
		return nil, infraerrors.BadRequest("INVALID_PLAN_PRICE", "subscription plan price cannot be negative")
	}

	user, err := s.userRepo.GetByID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("get user: %w", err)
	}
	if user.Status != payment.EntityStatusActive {
		return nil, infraerrors.Forbidden("USER_INACTIVE", "user account is disabled")
	}
	if user.Balance < plan.Price {
		return nil, ErrInsufficientBalance
	}

	tx, err := s.entClient.Tx(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	txCtx := dbent.NewTxContext(ctx, tx)

	if plan.Price > 0 {
		affected, err := tx.Client().User.Update().
			Where(dbuser.IDEQ(userID), dbuser.BalanceGTE(plan.Price)).
			AddBalance(-plan.Price).
			Save(txCtx)
		if err != nil {
			return nil, fmt.Errorf("deduct user balance: %w", err)
		}
		if affected == 0 {
			return nil, ErrInsufficientBalance
		}
	}

	redeemCode, err := s.createSubscriptionRedeemCodeForPlan(txCtx, plan)
	if err != nil {
		return nil, err
	}

	if err := s.redeemService.redeemRepo.Use(txCtx, redeemCode.ID, userID); err != nil {
		return nil, fmt.Errorf("mark subscription redeem code as used: %w", err)
	}
	now := time.Now()
	redeemCode.Status = StatusUsed
	redeemCode.UsedBy = &userID
	redeemCode.UsedAt = &now

	subscription, _, err := s.subscriptionSvc.AssignOrExtendSubscription(txCtx, &AssignSubscriptionInput{
		UserID:       userID,
		GroupID:      plan.GroupID,
		ValidityDays: redeemCode.ValidityDays,
		AssignedBy:   0,
		Notes:        fmt.Sprintf("通过灵石购买套餐「%s」并自动兑换，兑换码 %s", plan.Name, redeemCode.Code),
	})
	if err != nil {
		return nil, fmt.Errorf("assign or extend subscription: %w", err)
	}

	updatedUser, err := tx.Client().User.Query().
		Where(dbuser.IDEQ(userID)).
		Only(txCtx)
	if err != nil {
		return nil, fmt.Errorf("get updated user balance: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit subscription purchase transaction: %w", err)
	}

	s.invalidateBalanceSubscriptionPurchaseCaches(ctx, userID, redeemCode)

	return &PurchaseSubscriptionWithBalanceResult{
		PlanID:       int64(plan.ID),
		Price:        plan.Price,
		NewBalance:   updatedUser.Balance,
		RedeemCode:   redeemCode,
		Subscription: subscription,
	}, nil
}

func (s *PaymentService) createSubscriptionRedeemCodeForPlan(ctx context.Context, plan *dbent.SubscriptionPlan) (*RedeemCode, error) {
	const maxAttempts = 5
	validityDays := psComputeValidityDays(plan.ValidityDays, plan.ValidityUnit)
	if validityDays <= 0 {
		validityDays = 30
	}
	groupID := plan.GroupID

	for attempt := 0; attempt < maxAttempts; attempt++ {
		codeValue, err := GenerateRedeemCode()
		if err != nil {
			return nil, fmt.Errorf("generate subscription redeem code: %w", err)
		}
		code := &RedeemCode{
			Code:         strings.ToUpper(codeValue),
			Type:         RedeemTypeSubscription,
			Value:        plan.Price,
			Status:       StatusUnused,
			GroupID:      &groupID,
			ValidityDays: validityDays,
			Notes:        fmt.Sprintf("用户使用灵石购买订阅套餐：%s（套餐ID %d，价格 %.2f 灵石）", plan.Name, plan.ID, plan.Price),
		}
		if err := s.redeemService.redeemRepo.Create(ctx, code); err != nil {
			if dbent.IsConstraintError(err) {
				continue
			}
			return nil, fmt.Errorf("create subscription redeem code: %w", err)
		}
		return code, nil
	}

	return nil, fmt.Errorf("generate unique subscription redeem code: exhausted %d attempts", maxAttempts)
}

func (s *PaymentService) invalidateBalanceSubscriptionPurchaseCaches(ctx context.Context, userID int64, redeemCode *RedeemCode) {
	if s.redeemService == nil {
		return
	}
	s.redeemService.invalidateRedeemCaches(ctx, userID, redeemCode)
	if s.redeemService.authCacheInvalidator != nil {
		s.redeemService.authCacheInvalidator.InvalidateAuthCacheByUserID(ctx, userID)
	}
	if s.redeemService.billingCacheService == nil {
		return
	}
	go func() {
		cacheCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.redeemService.billingCacheService.InvalidateUserBalance(cacheCtx, userID)
	}()
}
