package handler

import (
	"context"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/handler/dto"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

// rechargeBonusConfigAPI 收窄 PlazaHandler 对支付配置 service 的依赖，便于单测注入替身。
type rechargeBonusConfigAPI interface {
	GetPaymentConfig(ctx context.Context) (*service.PaymentConfig, error)
}

// promoNowProvider 仅用于测试时注入"当前时间"。生产代码使用 time.Now。
type promoNowProvider func() time.Time

// GetPublicRechargePromo GET /api/v1/plaza/recharge-promo
//
// 匿名可访问端点，返回当前生效的充值优惠阶梯用于首页 banner 等公开 marketing surfaces。
//
// 行为约定：
//   - 支付未启用 / 余额充值关闭 / 阶梯为空 / 不在有效期 → `{ "promo": null }` HTTP 200
//   - 后端取数失败 → `{ "promo": null }` HTTP 200（silent skip，避免阻塞首页）
//   - 任何 Authorization 头被忽略，handler 不依赖 user context
func (h *PlazaHandler) GetPublicRechargePromo(c *gin.Context) {
	resp := dto.PublicRechargePromoResponseDTO{Promo: nil}
	if h.promoConfig == nil {
		response.Success(c, resp)
		return
	}

	now := time.Now()
	if h.promoNow != nil {
		now = h.promoNow()
	}

	cfg, err := h.promoConfig.GetPaymentConfig(c.Request.Context())
	if err != nil || cfg == nil || !cfg.Enabled || cfg.BalanceDisabled || !cfg.RechargeBonusActiveAt(now) {
		response.Success(c, resp)
		return
	}

	resp.Promo = publicRechargePromoFromConfig(cfg)
	response.Success(c, resp)
}

func publicRechargePromoFromConfig(cfg *service.PaymentConfig) *dto.PublicRechargePromoDTO {
	tiers := make([]dto.RechargeBonusTier, 0, len(cfg.RechargeBonusTiers))
	for _, t := range cfg.RechargeBonusTiers {
		tiers = append(tiers, dto.RechargeBonusTier{MinAmount: t.MinAmount, BonusPercent: t.BonusPercent})
	}
	mode, _ := service.NormalizeRechargeBonusMode(cfg.RechargeBonusMode)
	return &dto.PublicRechargePromoDTO{
		Mode:       mode,
		ValidFrom:  cfg.RechargeBonusValidFrom,
		ValidUntil: cfg.RechargeBonusValidUntil,
		Tiers:      tiers,
		Version:    cfg.RechargeBonusVersion(),
	}
}
