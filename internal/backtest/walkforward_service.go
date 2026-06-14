package backtest

import (
	"context"
	"errors"
	"sort"

	"github.com/shopspring/decimal"

	"github.com/tingz/easy-invest/internal/strategy"
)

// defaultBands 是未指定時掃描的再平衡區間候選。
var defaultBands = []decimal.Decimal{
	decimal.RequireFromString("0.02"),
	decimal.RequireFromString("0.03"),
	decimal.RequireFromString("0.05"),
	decimal.RequireFromString("0.08"),
	decimal.RequireFromString("0.10"),
}

// WalkForwardParams 是 walk-forward 驗證的 API 參數。
type WalkForwardParams struct {
	Symbols       []string                   `json:"symbols"`
	From          string                     `json:"from"`
	To            string                     `json:"to"`
	InitialCash   decimal.Decimal            `json:"initial_cash"`
	TargetWeights map[string]decimal.Decimal `json:"target_weights"`
	Bands         []decimal.Decimal          `json:"bands"`        // 掃描的再平衡區間；空值用預設
	TrainMonths   int                        `json:"train_months"` // 預設 24
	TestMonths    int                        `json:"test_months"`  // 預設 6
	StepMonths    int                        `json:"step_months"`  // 預設 = test_months
	SlippageBps   decimal.Decimal            `json:"slippage_bps"` // 預設 15
	Objective     string                     `json:"objective"`    // sharpe（預設）或 return
}

// FoldReport / WalkForwardReport 是序列化後的結果（金額與比率皆為字串）。
type FoldReport struct {
	TrainFrom   string `json:"train_from"`
	TrainTo     string `json:"train_to"`
	TestFrom    string `json:"test_from"`
	TestTo      string `json:"test_to"`
	ChosenLabel string `json:"chosen_label"`
	ChosenBand  string `json:"chosen_band"`
	ISObjective string `json:"is_objective"`
	OOSReturn   string `json:"oos_return"`
	OOSSharpe   string `json:"oos_sharpe"`
	Trades      int    `json:"trades"`
}

type WalkForwardReport struct {
	OOSReturn      string            `json:"oos_return"`
	OOSAnnualized  string            `json:"oos_annualized"`
	OOSSharpe      string            `json:"oos_sharpe"`
	OOSMaxDrawdown string            `json:"oos_max_drawdown"`
	PBO            string            `json:"pbo"`
	DeflatedSharpe string            `json:"deflated_sharpe"`
	ObservedSharpe string            `json:"observed_sharpe"`
	SelectedLabel  string            `json:"selected_label"`
	Trials         int               `json:"trials"`
	Folds          []FoldReport      `json:"folds"`
	OOSEquityCurve []equityPointJSON `json:"oos_equity_curve"`
	Assumptions    string            `json:"assumptions"`
}

// WalkForward 載入歷史行情與設定，掃描再平衡區間並執行 walk-forward 驗證。
// 純計算交給 WalkForward 核心；本方法只負責資料載入與序列化，不碰策略邏輯。
func (s *Service) WalkForward(ctx context.Context, userID string, params WalkForwardParams) (WalkForwardReport, error) {
	if params.InitialCash.LessThanOrEqual(decimal.Zero) {
		return WalkForwardReport{}, errors.New("initial_cash 必須大於 0")
	}
	settings, err := s.loadSettings(ctx, userID)
	if err != nil {
		return WalkForwardReport{}, err
	}
	weights := params.TargetWeights
	if len(weights) == 0 {
		weights = settings.TargetWeights
	}
	if len(weights) == 0 {
		return WalkForwardReport{}, errors.New("尚未設定目標權重，無法回測")
	}
	if params.SlippageBps.LessThanOrEqual(decimal.Zero) {
		params.SlippageBps = DefaultSlippageBps
	}
	bands := params.Bands
	if len(bands) == 0 {
		bands = defaultBands
	}

	symbols := params.Symbols
	if len(symbols) == 0 {
		for symbol := range weights {
			if symbol != "cash" {
				symbols = append(symbols, symbol)
			}
		}
	}
	sort.Strings(symbols)

	bars, assets, err := s.loadBars(ctx, symbols, params.From, params.To)
	if err != nil {
		return WalkForwardReport{}, err
	}
	if len(bars) == 0 {
		return WalkForwardReport{}, errors.New("指定區間內沒有任何標的的歷史行情")
	}

	grid := make([]ParamCandidate, 0, len(bands))
	for _, band := range bands {
		grid = append(grid, ParamCandidate{Label: "區間 " + band.String(), RebalanceBand: band})
	}

	res, err := WalkForward(WalkForwardInput{
		Bars:        bars,
		Assets:      assets,
		InitialCash: params.InitialCash,
		BaseSettings: strategy.Settings{
			TargetWeights:  weights,
			CashBuffer:     settings.CashBuffer,
			MinTradeAmount: settings.MinTradeAmount,
			PreferWholeLot: settings.PreferWholeLot,
		},
		Grid:      grid,
		Fees:      settings.Fees,
		Execution: Execution{FillTiming: "next_open", SlippageBps: params.SlippageBps},
		Window:    WindowSpec{TrainMonths: params.TrainMonths, TestMonths: params.TestMonths, StepMonths: params.StepMonths},
		Objective: params.Objective,
	})
	if err != nil {
		return WalkForwardReport{}, err
	}
	return toWalkForwardReport(res), nil
}

func toWalkForwardReport(res WalkForwardResult) WalkForwardReport {
	report := WalkForwardReport{
		OOSReturn:      res.OOSReturn.String(),
		OOSAnnualized:  res.OOSAnnualized.String(),
		OOSSharpe:      res.OOSSharpe.String(),
		OOSMaxDrawdown: res.OOSMaxDrawdown.String(),
		PBO:            res.PBO.String(),
		DeflatedSharpe: res.DeflatedSharpe.String(),
		ObservedSharpe: res.ObservedSharpe.String(),
		SelectedLabel:  res.SelectedLabel,
		Trials:         res.Trials,
		Assumptions: "每段以訓練期挑出最佳再平衡區間、緊接的測試期樣本外評估；" +
			"PBO 為過擬合機率（越低越好），通縮夏普已校正多重嘗試與非常態（> 0.95 才算顯著）。",
	}
	for _, f := range res.Folds {
		report.Folds = append(report.Folds, FoldReport{
			TrainFrom:   f.TrainFrom,
			TrainTo:     f.TrainTo,
			TestFrom:    f.TestFrom,
			TestTo:      f.TestTo,
			ChosenLabel: f.ChosenLabel,
			ChosenBand:  f.ChosenBand.String(),
			ISObjective: f.ISObjective.String(),
			OOSReturn:   f.OOSReturn.String(),
			OOSSharpe:   f.OOSSharpe.String(),
			Trades:      f.Trades,
		})
	}
	for _, pt := range res.OOSEquityCurve {
		report.OOSEquityCurve = append(report.OOSEquityCurve, equityPointJSON{Date: dateKey(pt.Date), Equity: pt.Equity.String()})
	}
	return report
}
