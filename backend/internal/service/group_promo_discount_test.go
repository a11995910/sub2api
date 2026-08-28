package service

import (
	"context"
	"math"
	"testing"
	"time"
)

// newPromoGroup 构造带活动折扣配置的分组，便于表驱动用例。
func promoFloatPtr(v float64) *float64 { return &v }

func newPromoGroup(enabled bool, start, end *time.Time, rate float64) *Group {
	return &Group{
		PromoDiscountEnabled: enabled,
		PromoDiscountStart:   start,
		PromoDiscountEnd:     end,
		PromoDiscountRate:    rate,
	}
}

func promoWindow() (*time.Time, *time.Time) {
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 9, 8, 0, 0, 0, 0, time.UTC)
	return &start, &end
}

func promoAt(day int, hour, min int) time.Time {
	return time.Date(2026, 9, day, hour, min, 0, 0, time.UTC)
}

func TestPromoDiscountMultiplierAt_DisabledOrUnconfigured(t *testing.T) {
	start, end := promoWindow()
	cases := []struct {
		name string
		g    *Group
	}{
		{"disabled", newPromoGroup(false, start, end, 0.95)},
		{"nil start", newPromoGroup(true, nil, end, 0.95)},
		{"nil end", newPromoGroup(true, start, nil, 0.95)},
		{"nil both", newPromoGroup(true, nil, nil, 0.95)},
		{"invalid rate zero", newPromoGroup(true, start, end, 0)},
		{"invalid rate above one", newPromoGroup(true, start, end, 1.5)},
		{"invalid rate negative", newPromoGroup(true, start, end, -0.95)},
		{"invalid rate NaN", newPromoGroup(true, start, end, math.NaN())},
		{"invalid rate Inf", newPromoGroup(true, start, end, math.Inf(1))},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.g.PromoDiscountMultiplierAt(promoAt(3, 12, 0)); got != 1.0 {
				t.Fatalf("expect 1.0, got %v", got)
			}
		})
	}
}

func TestPromoDiscountMultiplierAt_EndNotAfterStart(t *testing.T) {
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	cases := []struct {
		name string
		end  *time.Time
	}{
		{"end equals start", &start},
		{"end before start", func() *time.Time {
			e := start.Add(-time.Hour)
			return &e
		}()},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			g := newPromoGroup(true, &start, c.end, 0.95)
			if got := g.PromoDiscountMultiplierAt(promoAt(1, 1, 0)); got != 1.0 {
				t.Fatalf("expect 1.0 for illegal window, got %v", got)
			}
		})
	}
}

func TestPromoDiscountMultiplierAt_NilReceiver(t *testing.T) {
	var g *Group
	if got := g.PromoDiscountMultiplierAt(promoAt(3, 12, 0)); got != 1.0 {
		t.Fatalf("expect 1.0, got %v", got)
	}
}

func TestPromoDiscountMultiplierAt_Boundaries(t *testing.T) {
	start, end := promoWindow()
	g := newPromoGroup(true, start, end, 0.95)
	cases := []struct {
		t    time.Time
		want float64
	}{
		// 窗口为左闭右开 [start, end)：到期瞬间自动恢复原倍率。
		{promoAt(1, 0, 0), 0.95},  // start 当刻生效（含）
		{promoAt(3, 12, 0), 0.95}, // 窗口中段
		{promoAt(7, 23, 59), 0.95},
		{promoAt(8, 0, 0), 1.0}, // end 当刻失效（不含）
		{promoAt(8, 0, 1), 1.0},
		{promoAt(31, 23, 59), 1.0},
		{promoAt(1, 0, 0).Add(-time.Nanosecond), 1.0}, // start 前一瞬间
	}
	for _, c := range cases {
		t.Run(c.t.Format("01-02 15:04:05"), func(t *testing.T) {
			if got := g.PromoDiscountMultiplierAt(c.t); got != c.want {
				t.Fatalf("at %s: expect %v, got %v", c.t, c.want, got)
			}
		})
	}
}

func TestPromoDiscountMultiplierAt_AbsoluteWindowAcrossTimezones(t *testing.T) {
	// 活动窗口为绝对时间戳：任一时区表达的同一时刻判定结果必须一致。
	start, end := promoWindow()
	g := newPromoGroup(true, start, end, 0.95)
	sh, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}
	inWindow := time.Date(2026, 9, 3, 12, 0, 0, 0, sh)
	if got := g.PromoDiscountMultiplierAt(inWindow); got != 0.95 {
		t.Fatalf("same instant in Shanghai tz: expect 0.95, got %v", got)
	}
}

func TestPromoDiscountMultiplierAt_AppliesToAllGroupTypes(t *testing.T) {
	// 与高峰倍率不同：活动折扣适用于所有分组类型，标准分组同样生效。
	start, end := promoWindow()
	g := newPromoGroup(true, start, end, 0.95)
	g.SubscriptionType = "standard"
	if got := g.PromoDiscountMultiplierAt(promoAt(3, 12, 0)); got != 0.95 {
		t.Fatalf("standard group must also get promo factor, got %v", got)
	}
}

func TestValidatePromoDiscountConfig(t *testing.T) {
	start, end := promoWindow()
	validRate := 0.95
	cases := []struct {
		name    string
		enabled bool
		start   *time.Time
		end     *time.Time
		rate    *float64
		wantErr bool
	}{
		{"disabled passes through", false, nil, nil, nil, false},
		{"enabled valid", true, start, end, &validRate, false},
		{"enabled nil start", true, nil, end, &validRate, true},
		{"enabled nil end", true, start, nil, &validRate, true},
		{"enabled end before start", true, end, start, &validRate, true},
		{"enabled rate nil", true, start, end, nil, true},
		{"enabled rate zero", true, start, end, promoFloatPtr(0), true},
		{"enabled rate above one", true, start, end, promoFloatPtr(1.01), true},
		{"enabled rate one allowed", true, start, end, promoFloatPtr(1), false},
		{"enabled rate negative", true, start, end, promoFloatPtr(-0.5), true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rate := 0.0
			if c.rate != nil {
				rate = *c.rate
			}
			err := ValidatePromoDiscountConfig(c.enabled, c.start, c.end, rate)
			if c.wantErr && err == nil {
				t.Fatalf("expect error, got nil")
			}
			if !c.wantErr && err != nil {
				t.Fatalf("expect no error, got %v", err)
			}
		})
	}
}

func TestNormalizePromoDiscountConfig(t *testing.T) {
	start, end := promoWindow()
	t.Run("disabled cleans invalid rate", func(t *testing.T) {
		enabled, s, e, rate := NormalizePromoDiscountConfig(false, start, end, 1.5)
		if enabled || s != start || e != end || rate != 1.0 {
			t.Fatalf("disabled promo must keep window but clean rate: got %v %v %v %v", enabled, s, e, rate)
		}
	})
	t.Run("disabled keeps valid rate", func(t *testing.T) {
		_, _, _, rate := NormalizePromoDiscountConfig(false, start, end, 0.95)
		if rate != 0.95 {
			t.Fatalf("valid rate must be preserved when disabled: got %v", rate)
		}
	})
	t.Run("enabled passes through", func(t *testing.T) {
		enabled, s, e, rate := NormalizePromoDiscountConfig(true, start, end, 0.95)
		if !enabled || s != start || e != end || rate != 0.95 {
			t.Fatalf("enabled promo must pass through unchanged: got %v %v %v %v", enabled, s, e, rate)
		}
	})
}

// TestPromoDiscount_GatewayBillingSequence 验证 computePeakAwareMultipliers 的叠加顺序：
// token 倍率 = base × 高峰因子 × 活动折扣；图片按次倍率不受高峰影响、但活动折扣作为分组
// 整体让利同样乘入。若有人调换叠加顺序或遗漏折扣，此测试会失败。
func TestPromoDiscount_GatewayBillingSequence(t *testing.T) {
	const baseMultiplier = 0.8
	start, end := promoWindow()
	promo := newPromoGroup(true, start, end, 0.95)
	promo.SubscriptionType = "subscription"
	promo.PeakRateEnabled = true
	promo.PeakStart = "14:00"
	promo.PeakEnd = "18:00"
	promo.PeakRateMultiplier = 3.0
	apiKey := &APIKey{Group: promo}
	approxEq := func(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

	t.Run("in window with peak", func(t *testing.T) {
		// 9月3日 15:30 UTC：活动窗口内且处于高峰时段。
		now := promoAt(3, 15, 30)
		tokenMultiplier, imageMultiplier := computePeakAwareMultipliers(apiKey, baseMultiplier, now)
		if want := baseMultiplier * 3.0 * 0.95; !approxEq(tokenMultiplier, want) {
			t.Fatalf("token multiplier should include peak*promo: got %v, want %v", tokenMultiplier, want)
		}
		if want := baseMultiplier * 0.95; !approxEq(imageMultiplier, want) {
			t.Fatalf("image multiplier should include promo only: got %v, want %v", imageMultiplier, want)
		}
	})

	t.Run("in window off peak", func(t *testing.T) {
		now := promoAt(3, 20, 0)
		tokenMultiplier, imageMultiplier := computePeakAwareMultipliers(apiKey, baseMultiplier, now)
		if want := baseMultiplier * 0.95; !approxEq(tokenMultiplier, want) {
			t.Fatalf("token multiplier off peak: got %v, want %v", tokenMultiplier, want)
		}
		if want := baseMultiplier * 0.95; !approxEq(imageMultiplier, want) {
			t.Fatalf("image multiplier off peak: got %v, want %v", imageMultiplier, want)
		}
	})

	t.Run("after window restores base", func(t *testing.T) {
		now := promoAt(9, 12, 0)
		tokenMultiplier, imageMultiplier := computePeakAwareMultipliers(apiKey, baseMultiplier, now)
		if !approxEq(tokenMultiplier, baseMultiplier) {
			t.Fatalf("token multiplier after window: got %v, want %v", tokenMultiplier, baseMultiplier)
		}
		if !approxEq(imageMultiplier, baseMultiplier) {
			t.Fatalf("image multiplier after window: got %v, want %v", imageMultiplier, baseMultiplier)
		}
	})
}

// TestPromoDiscount_SnapshotRoundTrip 防回归：认证缓存快照必须携带活动折扣 4 字段，
// 否则扣费路径拿到的 apiKey.Group 缺字段、PromoDiscountMultiplierAt 恒降级为 1.0。
func TestPromoDiscount_SnapshotRoundTrip(t *testing.T) {
	start, end := promoWindow()
	apiKey := &APIKey{
		User:  &User{ID: 1, Status: StatusActive, Role: RoleUser},
		Group: newPromoGroup(true, start, end, 0.95),
	}
	svc := &APIKeyService{}

	snapshot := svc.snapshotFromAPIKey(context.Background(), apiKey)
	if snapshot == nil || snapshot.Group == nil {
		t.Fatalf("snapshot or snapshot.Group must not be nil")
	}
	restored := svc.snapshotToAPIKey("k", snapshot)
	if restored.Group == nil {
		t.Fatalf("restored.Group must not be nil")
	}

	g := restored.Group
	if !g.PromoDiscountEnabled || g.PromoDiscountStart == nil || g.PromoDiscountEnd == nil || g.PromoDiscountRate != 0.95 {
		t.Fatalf("promo fields lost in snapshot round-trip: %+v", g)
	}
	if got := g.PromoDiscountMultiplierAt(promoAt(3, 12, 0)); got != 0.95 {
		t.Fatalf("in-window multiplier after round-trip: got %v, want 0.95", got)
	}
	if got := g.PromoDiscountMultiplierAt(promoAt(9, 12, 0)); got != 1.0 {
		t.Fatalf("after-window multiplier after round-trip: got %v, want 1.0", got)
	}
}
