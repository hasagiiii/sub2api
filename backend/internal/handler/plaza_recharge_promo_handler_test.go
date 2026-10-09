//go:build unit

package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type rechargeBonusConfigStub struct {
	cfg *service.PaymentConfig
	err error
}

func (s *rechargeBonusConfigStub) GetPaymentConfig(context.Context) (*service.PaymentConfig, error) {
	return s.cfg, s.err
}

type publicRechargePromoBody struct {
	Data struct {
		Promo *struct {
			Mode       string `json:"mode"`
			ValidFrom  string `json:"valid_from"`
			ValidUntil string `json:"valid_until"`
			Tiers      []struct {
				MinAmount    float64 `json:"min_amount"`
				BonusPercent float64 `json:"bonus_percent"`
			} `json:"tiers"`
			Version string `json:"version"`
		} `json:"promo"`
	} `json:"data"`
}

func callPublicRechargePromo(t *testing.T, cfg *service.PaymentConfig, err error, now time.Time) publicRechargePromoBody {
	t.Helper()
	gin.SetMode(gin.TestMode)
	h := &PlazaHandler{promoConfig: &rechargeBonusConfigStub{cfg: cfg, err: err}, promoNow: func() time.Time { return now }}
	r := gin.New()
	r.GET("/plaza/recharge-promo", h.GetPublicRechargePromo)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/plaza/recharge-promo", nil)
	req.Header.Set("Authorization", "Bearer ignored")
	r.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	var body publicRechargePromoBody
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	return body
}

func TestPublicRechargePromo(t *testing.T) {
	now := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	from := now.Add(-time.Hour)
	until := now.Add(24 * time.Hour)
	active := func() *service.PaymentConfig {
		return &service.PaymentConfig{
			Enabled:                 true,
			RechargeBonusTiers:      []service.RechargeBonusTier{{MinAmount: 100, BonusPercent: 10}},
			RechargeBonusMode:       service.RechargeBonusModeDiscount,
			RechargeBonusNotice:     "**国庆特惠**",
			RechargeBonusValidFrom:  &from,
			RechargeBonusValidUntil: &until,
		}
	}

	t.Run("active window returns promo", func(t *testing.T) {
		cfg := active()
		body := callPublicRechargePromo(t, cfg, nil, now)
		require.NotNil(t, body.Data.Promo)
		require.Equal(t, "discount", body.Data.Promo.Mode)
		require.Equal(t, until.Format(time.RFC3339), body.Data.Promo.ValidUntil)
		require.Len(t, body.Data.Promo.Tiers, 1)
		require.Equal(t, 10.0, body.Data.Promo.Tiers[0].BonusPercent)
		require.Equal(t, cfg.RechargeBonusVersion(), body.Data.Promo.Version)
	})

	nullCases := map[string]func() (*service.PaymentConfig, error, time.Time){
		"ended":       func() (*service.PaymentConfig, error, time.Time) { return active(), nil, until },
		"not started": func() (*service.PaymentConfig, error, time.Time) { return active(), nil, from.Add(-time.Second) },
		"payment disabled": func() (*service.PaymentConfig, error, time.Time) {
			c := active()
			c.Enabled = false
			return c, nil, now
		},
		"balance disabled": func() (*service.PaymentConfig, error, time.Time) {
			c := active()
			c.BalanceDisabled = true
			return c, nil, now
		},
		"no tiers": func() (*service.PaymentConfig, error, time.Time) {
			c := active()
			c.RechargeBonusTiers = nil
			return c, nil, now
		},
		"config error": func() (*service.PaymentConfig, error, time.Time) { return nil, errors.New("boom"), now },
	}
	for name, build := range nullCases {
		t.Run(name+" returns null", func(t *testing.T) {
			cfg, err, at := build()
			require.Nil(t, callPublicRechargePromo(t, cfg, err, at).Data.Promo)
		})
	}
}
