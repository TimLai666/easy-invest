package api

import (
	"context"
	"encoding/json"

	"github.com/tingz/easy-invest/internal/num"
)

type settingsResponse struct {
	FeeRate             string         `json:"fee_rate"`
	FeeDiscount         string         `json:"fee_discount"`
	FeeMinimum          string         `json:"fee_minimum"`
	DividendTransferFee string         `json:"dividend_transfer_fee"`
	CashBuffer          string         `json:"cash_buffer"`
	MinTradeAmount      string         `json:"min_trade_amount"`
	PreferWholeLot      bool           `json:"prefer_whole_lot"`
	RiskProfile         string         `json:"risk_profile"`
	TargetWeights       map[string]any `json:"target_weights"`
	RebalanceBand       string         `json:"rebalance_band"`
	UpdatedAt           string         `json:"updated_at"`
}

func (s *Server) readSettings(ctx context.Context, userID string) (settingsResponse, error) {
	var resp settingsResponse
	var targetRaw []byte
	err := s.db.QueryRow(ctx, `
		SELECT fee_rate::text, fee_discount::text, fee_minimum::text, dividend_transfer_fee::text,
		       cash_buffer::text, min_trade_amount::text, prefer_whole_lot, risk_profile,
		       target_weights, rebalance_band::text, updated_at::text
		FROM user_settings WHERE user_id = $1
	`, userID).Scan(&resp.FeeRate, &resp.FeeDiscount, &resp.FeeMinimum, &resp.DividendTransferFee,
		&resp.CashBuffer, &resp.MinTradeAmount, &resp.PreferWholeLot, &resp.RiskProfile,
		&targetRaw, &resp.RebalanceBand, &resp.UpdatedAt)
	if err != nil {
		return settingsResponse{}, err
	}
	_ = json.Unmarshal(targetRaw, &resp.TargetWeights)
	if resp.TargetWeights == nil {
		resp.TargetWeights = map[string]any{}
	}
	resp.FeeRate = num.Parse(resp.FeeRate).String()
	resp.FeeDiscount = num.Parse(resp.FeeDiscount).String()
	resp.FeeMinimum = num.Parse(resp.FeeMinimum).String()
	resp.DividendTransferFee = num.Parse(resp.DividendTransferFee).String()
	resp.CashBuffer = num.Parse(resp.CashBuffer).String()
	resp.MinTradeAmount = num.Parse(resp.MinTradeAmount).String()
	resp.RebalanceBand = num.Parse(resp.RebalanceBand).String()
	return resp, nil
}
