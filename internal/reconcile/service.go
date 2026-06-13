package reconcile

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	"github.com/tingz/easy-invest/internal/ledger"
	"github.com/tingz/easy-invest/internal/num"
	"github.com/tingz/easy-invest/internal/twmarket"
)

// 台灣不實施夏令時間，固定 +8 即可，避免依賴系統時區資料庫。
var taipeiLocation = time.FixedZone("Asia/Taipei", 8*60*60)

var (
	// ErrAlreadyResolved 差異已處理過，不可重複 resolve。
	ErrAlreadyResolved = errors.New("差異已處理過，不可重複 resolve")
	// ErrInvalidResolution resolution 不在允許值內。
	ErrInvalidResolution = errors.New("resolution 必須是 adjusted、accepted_as_is 或 fixed_input")
)

type Service struct {
	db     *pgxpool.Pool
	ledger *ledger.Service
}

type BrokerPositionInput struct {
	Symbol         string         `json:"symbol"`
	QuantityShares string         `json:"quantity_shares"`
	BrokerAvgCost  *string        `json:"broker_avg_cost"`
	Details        map[string]any `json:"details"`
}

type CreateSnapshotInput struct {
	UserID     string                `json:"-"`
	BrokerName string                `json:"broker_name"`
	Source     string                `json:"source"`
	CapturedAt *time.Time            `json:"captured_at"`
	Positions  []BrokerPositionInput `json:"positions"`
	RawPayload map[string]any        `json:"raw_payload"`
}

type Snapshot struct {
	ID         string `json:"id"`
	BrokerName string `json:"broker_name"`
	Source     string `json:"source"`
	CapturedAt string `json:"captured_at"`
	CreatedAt  string `json:"created_at"`
}

type Diff struct {
	ID                string  `json:"id"`
	RunID             string  `json:"run_id"`
	AssetID           *string `json:"asset_id,omitempty"`
	Symbol            *string `json:"symbol,omitempty"`
	DiffType          string  `json:"diff_type"`
	InternalValue     *string `json:"internal_value,omitempty"`
	BrokerValue       *string `json:"broker_value,omitempty"`
	Resolution        string  `json:"resolution"`
	AdjustmentEventID *string `json:"adjustment_event_id,omitempty"`
	ResolvedAt        *string `json:"resolved_at,omitempty"`
}

type Run struct {
	ID               string `json:"id"`
	BrokerSnapshotID string `json:"broker_snapshot_id"`
	Status           string `json:"status"`
	CreatedAt        string `json:"created_at"`
	Diffs            []Diff `json:"diffs,omitempty"`
}

// AdjustmentInput resolution=adjusted 時，呼叫端可帶入的調整事件欄位。
// 留空的欄位由系統依 diff 內容推斷（見 buildAdjustmentEvent）。
type AdjustmentInput struct {
	EventType   string         `json:"event_type"`
	TradeDate   string         `json:"trade_date"`
	Quantity    string         `json:"quantity"`
	Unit        string         `json:"unit"`
	Price       string         `json:"price"`
	GrossAmount *string        `json:"gross_amount"`
	Fee         *string        `json:"fee"`
	Tax         *string        `json:"tax"`
	Notes       string         `json:"notes"`
	Metadata    map[string]any `json:"metadata"`
}

// ResolveDiffInput 對應 docs/api-spec.md 的 resolve body：
// {"resolution": "adjusted", "adjustment": {…ledger event 欄位…}}。
type ResolveDiffInput struct {
	Resolution string           `json:"resolution"`
	Adjustment *AdjustmentInput `json:"adjustment"`
}

func NewService(db *pgxpool.Pool, ledgerSvc *ledger.Service) *Service {
	return &Service{db: db, ledger: ledgerSvc}
}

func (s *Service) CreateBrokerSnapshot(ctx context.Context, input CreateSnapshotInput) (Snapshot, error) {
	if input.BrokerName == "" {
		return Snapshot{}, errors.New("broker_name required")
	}
	source := input.Source
	if source == "" {
		source = "json_import"
	}
	capturedAt := time.Now()
	if input.CapturedAt != nil {
		capturedAt = *input.CapturedAt
	}
	raw := input.RawPayload
	if raw == nil {
		raw = map[string]any{"positions": input.Positions}
	}
	rawJSON, _ := json.Marshal(raw)

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return Snapshot{}, err
	}
	defer tx.Rollback(ctx)

	var snapshot Snapshot
	err = tx.QueryRow(ctx, `
		INSERT INTO broker_snapshots (user_id, broker_name, source, captured_at, raw_payload)
		VALUES ($1, $2, $3, $4, $5::jsonb)
		RETURNING id::text, broker_name, source, captured_at::text, created_at::text
	`, input.UserID, input.BrokerName, source, capturedAt, string(rawJSON)).
		Scan(&snapshot.ID, &snapshot.BrokerName, &snapshot.Source, &snapshot.CapturedAt, &snapshot.CreatedAt)
	if err != nil {
		return Snapshot{}, err
	}
	for _, p := range input.Positions {
		details := p.Details
		if details == nil {
			details = map[string]any{}
		}
		detailsJSON, _ := json.Marshal(details)
		var avg any
		if p.BrokerAvgCost != nil {
			avg = *p.BrokerAvgCost
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO broker_positions (snapshot_id, symbol, quantity_shares, broker_avg_cost, details)
			VALUES ($1, $2, $3::numeric, $4::numeric, $5::jsonb)
		`, snapshot.ID, p.Symbol, p.QuantityShares, avg, string(detailsJSON)); err != nil {
			return Snapshot{}, err
		}
	}
	return snapshot, tx.Commit(ctx)
}

// CreateBrokerSnapshotFromCSV 用註冊的 per-broker 解析器解析 CSV 後建立快照。
// 原始 CSV 內容完整保留在 raw_payload，之後可追溯匯入來源。
func (s *Service) CreateBrokerSnapshotFromCSV(ctx context.Context, userID, brokerName, parserName string, capturedAt *time.Time, r io.Reader) (Snapshot, error) {
	rawCSV, err := io.ReadAll(r)
	if err != nil {
		return Snapshot{}, fmt.Errorf("讀取 CSV 失敗：%w", err)
	}
	positions, err := ParsePositionsCSV(parserName, bytes.NewReader(rawCSV))
	if err != nil {
		return Snapshot{}, err
	}
	return s.CreateBrokerSnapshot(ctx, CreateSnapshotInput{
		UserID:     userID,
		BrokerName: brokerName,
		Source:     "csv_import",
		CapturedAt: capturedAt,
		Positions:  positions,
		RawPayload: map[string]any{
			"parser": parserName,
			"csv":    string(rawCSV),
		},
	})
}

func (s *Service) ListBrokerSnapshots(ctx context.Context, userID string) ([]Snapshot, error) {
	rows, err := s.db.Query(ctx, `
		SELECT id::text, broker_name, source, captured_at::text, created_at::text
		FROM broker_snapshots
		WHERE user_id = $1
		ORDER BY captured_at DESC
		LIMIT 100
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var snapshots []Snapshot
	for rows.Next() {
		var snapshot Snapshot
		if err := rows.Scan(&snapshot.ID, &snapshot.BrokerName, &snapshot.Source, &snapshot.CapturedAt, &snapshot.CreatedAt); err != nil {
			return nil, err
		}
		snapshots = append(snapshots, snapshot)
	}
	return snapshots, rows.Err()
}

func (s *Service) CreateRun(ctx context.Context, userID, snapshotID string) (Run, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return Run{}, err
	}
	defer tx.Rollback(ctx)
	var run Run
	err = tx.QueryRow(ctx, `
		INSERT INTO reconciliation_runs (user_id, broker_snapshot_id)
		SELECT $1, id FROM broker_snapshots WHERE id = $2 AND user_id = $1
		RETURNING id::text, broker_snapshot_id::text, status, created_at::text
	`, userID, snapshotID).Scan(&run.ID, &run.BrokerSnapshotID, &run.Status, &run.CreatedAt)
	if err != nil {
		return Run{}, err
	}

	// 1. 內部視角：庫存（含調整後平均成本）與各資產費稅合計。
	portfolio, err := s.ledger.Portfolio(ctx, userID)
	if err != nil {
		return Run{}, err
	}
	feeTax, err := s.internalFeeTaxByAsset(ctx, userID)
	if err != nil {
		return Run{}, err
	}
	var internal []InternalPosition
	for _, p := range portfolio.Positions {
		pos := InternalPosition{
			AssetID:        p.AssetID,
			Symbol:         p.Symbol,
			QuantityShares: num.Parse(p.QuantityShares),
		}
		if p.AdjustedAverageCost != "" {
			pos.AdjustedAvgCost = num.Parse(p.AdjustedAverageCost)
			pos.HasAvgCost = true
		}
		if total, ok := feeTax[p.AssetID]; ok {
			pos.TotalFee = total.fee
			pos.TotalTax = total.tax
		}
		internal = append(internal, pos)
	}

	// 2. 券商視角：快照部位（含均價與 details 內的費稅欄位）。
	broker, brokerSymbols, err := s.brokerPositionRows(ctx, tx, snapshotID)
	if err != nil {
		return Run{}, err
	}

	// 3. 純邏輯引擎比對部位差異。
	diffs := ComputePositionDiffs(internal, broker)

	// 4. 漏記配息：公司行動 vs ledger 內 cash_dividend 事件。
	candidates, err := s.dividendCandidates(ctx, userID, "")
	if err != nil {
		return Run{}, err
	}
	recorded, err := s.recordedDividends(ctx, userID, "")
	if err != nil {
		return Run{}, err
	}
	diffs = append(diffs, ComputeMissingDividends(candidates, recorded)...)

	// 內部沒有部位的券商代號，盡量補上 assets 對應，diff 才查得到 symbol。
	assetIDBySymbol, err := s.assetIDsBySymbol(ctx, brokerSymbols)
	if err != nil {
		return Run{}, err
	}
	for _, d := range diffs {
		assetID := d.AssetID
		if assetID == "" {
			assetID = assetIDBySymbol[d.Symbol]
		}
		inserted, err := s.insertDiff(ctx, tx, run.ID, assetID, d.DiffType, d.InternalValue, d.BrokerValue)
		if err != nil {
			return Run{}, err
		}
		run.Diffs = append(run.Diffs, inserted)
	}

	// 完全沒有差異的 run 直接視為 resolved，不留懸而未決的紀錄。
	if len(run.Diffs) == 0 {
		if _, err := tx.Exec(ctx, `
			UPDATE reconciliation_runs SET status = 'resolved' WHERE id = $1 AND user_id = $2
		`, run.ID, userID); err != nil {
			return Run{}, err
		}
		run.Status = "resolved"
	}
	return run, tx.Commit(ctx)
}

func (s *Service) GetRun(ctx context.Context, userID, runID string) (Run, error) {
	var run Run
	err := s.db.QueryRow(ctx, `
		SELECT id::text, broker_snapshot_id::text, status, created_at::text
		FROM reconciliation_runs
		WHERE user_id = $1 AND id = $2
	`, userID, runID).Scan(&run.ID, &run.BrokerSnapshotID, &run.Status, &run.CreatedAt)
	if err != nil {
		return Run{}, err
	}
	diffs, err := s.diffs(ctx, runID)
	if err != nil {
		return Run{}, err
	}
	run.Diffs = diffs
	return run, nil
}

// ResolveDiff 舊簽名相容包裝：只帶 resolution，不帶 adjustment 內容。
func (s *Service) ResolveDiff(ctx context.Context, userID, diffID, resolution string) (Diff, error) {
	return s.ResolveDiffWithInput(ctx, userID, diffID, ResolveDiffInput{Resolution: resolution})
}

// ResolveDiffWithInput 處理一筆對帳差異：
//   - adjusted：建立 ledger 調整事件（漏記配息建 cash_dividend，其餘建
//     broker_reconciliation_adjustment），回填 adjustment_event_id 與 resolved_at。
//     注意：ledger.Service 公開 API 不支援外部交易，所以是「先建事件、再更新 diff」兩步；
//     第二步失敗時會盡力 void 剛建立的事件做補償。
//   - accepted_as_is / fixed_input：只標記狀態與 resolved_at。
//
// 全部差異都處理完之後，run.status 自動轉成 resolved。
func (s *Service) ResolveDiffWithInput(ctx context.Context, userID, diffID string, input ResolveDiffInput) (Diff, error) {
	if input.Resolution != "accepted_as_is" && input.Resolution != "fixed_input" && input.Resolution != "adjusted" {
		return Diff{}, ErrInvalidResolution
	}
	current, runID, err := s.diffForUser(ctx, userID, diffID)
	if err != nil {
		return Diff{}, err
	}
	if current.Resolution != "pending" {
		return Diff{}, ErrAlreadyResolved
	}

	var adjustmentEventID string
	if input.Resolution == "adjusted" {
		eventInput, err := s.buildAdjustmentEvent(ctx, userID, runID, current, input.Adjustment)
		if err != nil {
			return Diff{}, err
		}
		created, err := s.ledger.CreateEvent(ctx, eventInput)
		if err != nil {
			return Diff{}, fmt.Errorf("建立調整事件失敗：%w", err)
		}
		adjustmentEventID = created.ID
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return Diff{}, s.compensateAdjustment(ctx, userID, adjustmentEventID, err)
	}
	defer tx.Rollback(ctx)

	if adjustmentEventID != "" {
		// 補回 ledger_events.reconciliation_run_id 鏈結欄位。
		// ledger 公開 API 目前沒有這個參數；這裡只在事件剛建立、欄位仍為 NULL 時補值，
		// 不碰任何金額、數量等業務欄位。
		if _, err := tx.Exec(ctx, `
			UPDATE ledger_events
			SET reconciliation_run_id = $3
			WHERE id = $1 AND user_id = $2 AND reconciliation_run_id IS NULL
		`, adjustmentEventID, userID, runID); err != nil {
			return Diff{}, s.compensateAdjustment(ctx, userID, adjustmentEventID, err)
		}
	}

	tag, err := tx.Exec(ctx, `
		UPDATE reconciliation_diffs d
		SET resolution = $3,
		    resolved_at = now(),
		    adjustment_event_id = NULLIF($4, '')::uuid
		FROM reconciliation_runs r
		WHERE d.run_id = r.id AND r.user_id = $1 AND d.id = $2 AND d.resolution = 'pending'
	`, userID, diffID, input.Resolution, adjustmentEventID)
	if err != nil {
		return Diff{}, s.compensateAdjustment(ctx, userID, adjustmentEventID, err)
	}
	if tag.RowsAffected() == 0 {
		return Diff{}, s.compensateAdjustment(ctx, userID, adjustmentEventID, ErrAlreadyResolved)
	}
	if err := s.maybeResolveRun(ctx, tx, userID, runID); err != nil {
		return Diff{}, s.compensateAdjustment(ctx, userID, adjustmentEventID, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Diff{}, s.compensateAdjustment(ctx, userID, adjustmentEventID, err)
	}
	return s.diff(ctx, diffID)
}

// buildAdjustmentEvent 依差異內容組出 ledger 調整事件輸入。
// 呼叫端有帶 adjustment 欄位就優先採用，缺的欄位由 diff 推斷預設值。
func (s *Service) buildAdjustmentEvent(ctx context.Context, userID, runID string, diff Diff, adj *AdjustmentInput) (ledger.CreateEventInput, error) {
	eventType := ledger.EventBrokerReconciliationAdjustment
	if diff.DiffType == DiffMissingDividend {
		eventType = ledger.EventCashDividend
	}
	tradeDate := ""
	var grossAmount *string
	notes := fmt.Sprintf("券商對帳調整：%s", diff.DiffType)
	metadata := map[string]any{
		"reconciliation_run_id":  runID,
		"reconciliation_diff_id": diff.ID,
		"diff_type":              diff.DiffType,
	}

	if adj != nil {
		if adj.EventType != "" {
			eventType = adj.EventType
		}
		tradeDate = adj.TradeDate
		grossAmount = adj.GrossAmount
		if adj.Notes != "" {
			notes = notes + "；" + adj.Notes
		}
		for k, v := range adj.Metadata {
			if _, exists := metadata[k]; !exists {
				metadata[k] = v
			}
		}
	}

	symbol := ""
	if diff.Symbol != nil {
		symbol = *diff.Symbol
	}

	// 漏記配息：呼叫端沒給金額或日期時，從公司行動重新推導應收股利。
	if diff.DiffType == DiffMissingDividend && (grossAmount == nil || tradeDate == "") {
		candidate, found, err := s.findUnrecordedDividend(ctx, userID, diff)
		if err != nil {
			return ledger.CreateEventInput{}, err
		}
		if found {
			if grossAmount == nil {
				amount := EstimatedDividendAmount(candidate).String()
				grossAmount = &amount
			}
			if tradeDate == "" {
				when := candidate.ExDate
				if candidate.PayDate != nil {
					when = *candidate.PayDate
				}
				tradeDate = when.Format("2006-01-02")
			}
			metadata["corporate_action_id"] = candidate.ActionID
		} else if grossAmount == nil && diff.BrokerValue != nil {
			// 找不到對應公司行動（可能已被其他調整吃掉），退回用 diff 上記錄的估計值。
			grossAmount = diff.BrokerValue
		}
	}
	if eventType == ledger.EventCashDividend && symbol == "" {
		return ledger.CreateEventInput{}, errors.New("此差異沒有對應資產，請在 adjustment 內指定事件內容")
	}
	if eventType == ledger.EventCashDividend && grossAmount == nil {
		return ledger.CreateEventInput{}, errors.New("推導不出股利金額，請在 adjustment 內帶 gross_amount")
	}
	if tradeDate == "" {
		tradeDate = time.Now().In(taipeiLocation).Format("2006-01-02")
	}

	input := ledger.CreateEventInput{
		UserID:      userID,
		EventType:   eventType,
		Symbol:      symbol,
		TradeDate:   tradeDate,
		GrossAmount: grossAmount,
		Source:      "reconciliation",
		SourceRef:   &diff.ID,
		Notes:       notes,
		Metadata:    metadata,
	}
	if adj != nil {
		input.Quantity = adj.Quantity
		input.Unit = adj.Unit
		if input.Quantity != "" && input.Unit == "" {
			input.Unit = twmarket.UnitShare
		}
		input.Price = adj.Price
		input.Fee = adj.Fee
		input.Tax = adj.Tax
	}
	return input, nil
}

// findUnrecordedDividend 重新跑一次該資產的漏配息比對，挑出與 diff 對應的候選：
// 優先找估計金額與 diff.broker_value 相同者，否則取除息日最早的一筆。
func (s *Service) findUnrecordedDividend(ctx context.Context, userID string, diff Diff) (DividendCandidate, bool, error) {
	if diff.AssetID == nil {
		return DividendCandidate{}, false, nil
	}
	candidates, err := s.dividendCandidates(ctx, userID, *diff.AssetID)
	if err != nil {
		return DividendCandidate{}, false, err
	}
	recorded, err := s.recordedDividends(ctx, userID, *diff.AssetID)
	if err != nil {
		return DividendCandidate{}, false, err
	}
	missing := UnmatchedDividends(candidates, recorded)
	if len(missing) == 0 {
		return DividendCandidate{}, false, nil
	}
	if diff.BrokerValue != nil {
		want := num.Parse(*diff.BrokerValue)
		for _, c := range missing {
			if EstimatedDividendAmount(c).Equal(want) {
				return c, true, nil
			}
		}
	}
	return missing[0], true, nil
}

// compensateAdjustment resolve 第二步（更新 diff）失敗時的補償：
// 盡力 void 剛建立的調整事件，避免留下無人認領的孤兒事件。void 也失敗時把兩個錯誤都帶出去。
func (s *Service) compensateAdjustment(ctx context.Context, userID, adjustmentEventID string, cause error) error {
	if adjustmentEventID == "" {
		return cause
	}
	if voidErr := s.ledger.VoidEvent(ctx, userID, adjustmentEventID, "對帳差異更新失敗，自動作廢調整事件"); voidErr != nil {
		return fmt.Errorf("更新差異失敗（%w），且補償作廢調整事件 %s 也失敗：%v", cause, adjustmentEventID, voidErr)
	}
	return fmt.Errorf("更新差異失敗，調整事件已自動作廢：%w", cause)
}

// maybeResolveRun 該 run 已無 pending 差異時，自動把狀態轉成 resolved。
func (s *Service) maybeResolveRun(ctx context.Context, tx pgx.Tx, userID, runID string) error {
	_, err := tx.Exec(ctx, `
		UPDATE reconciliation_runs r
		SET status = 'resolved'
		WHERE r.id = $2 AND r.user_id = $1 AND r.status = 'open'
		  AND NOT EXISTS (
			SELECT 1 FROM reconciliation_diffs d
			WHERE d.run_id = r.id AND d.resolution = 'pending'
		  )
	`, userID, runID)
	return err
}

type feeTaxTotal struct {
	fee decimal.Decimal
	tax decimal.Decimal
}

// internalFeeTaxByAsset 各資產所有未作廢事件的手續費與稅費合計。
func (s *Service) internalFeeTaxByAsset(ctx context.Context, userID string) (map[string]feeTaxTotal, error) {
	rows, err := s.db.Query(ctx, `
		SELECT asset_id::text, COALESCE(SUM(fee), 0)::text, COALESCE(SUM(tax), 0)::text
		FROM ledger_events
		WHERE user_id = $1 AND voided_at IS NULL AND asset_id IS NOT NULL
		GROUP BY asset_id
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	totals := map[string]feeTaxTotal{}
	for rows.Next() {
		var assetID, feeText, taxText string
		if err := rows.Scan(&assetID, &feeText, &taxText); err != nil {
			return nil, err
		}
		totals[assetID] = feeTaxTotal{fee: num.Parse(feeText), tax: num.Parse(taxText)}
	}
	return totals, rows.Err()
}

// brokerPositionRows 讀出快照部位並轉成引擎輸入；details 內的費稅欄位一併帶出。
func (s *Service) brokerPositionRows(ctx context.Context, tx pgx.Tx, snapshotID string) ([]BrokerPositionRow, []string, error) {
	rows, err := tx.Query(ctx, `
		SELECT symbol, quantity_shares::text, broker_avg_cost::text, details
		FROM broker_positions
		WHERE snapshot_id = $1
		ORDER BY symbol
	`, snapshotID)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	var broker []BrokerPositionRow
	var symbols []string
	for rows.Next() {
		var symbol, qtyText string
		var avgCost sql.NullString
		var details []byte
		if err := rows.Scan(&symbol, &qtyText, &avgCost, &details); err != nil {
			return nil, nil, err
		}
		row := BrokerPositionRow{Symbol: symbol, QuantityShares: num.Parse(qtyText)}
		if avgCost.Valid {
			cost := num.Parse(avgCost.String)
			row.AvgCost = &cost
		}
		detailMap := decodeDetails(details)
		row.TotalFee = detailsDecimal(detailMap, "total_fee", "fee_total", "fee")
		row.TotalTax = detailsDecimal(detailMap, "total_tax", "tax_total", "tax")
		broker = append(broker, row)
		symbols = append(symbols, symbol)
	}
	return broker, symbols, rows.Err()
}

// dividendCandidates 該使用者持有過的資產裡，公司行動內的現金股利，
// 並順便算好除息日前一日的持有股數（trade_date < ex_date 的數量加總）。
// assetID 非空時只查單一資產（resolve 重推導用）。
func (s *Service) dividendCandidates(ctx context.Context, userID, assetID string) ([]DividendCandidate, error) {
	rows, err := s.db.Query(ctx, `
		SELECT ca.id::text, ca.asset_id::text, a.symbol, ca.ex_date::text, ca.pay_date::text, ca.cash_per_share::text,
		       COALESCE((
		           SELECT SUM(e.quantity_shares)
		           FROM ledger_events e
		           WHERE e.user_id = $1 AND e.asset_id = ca.asset_id
		             AND e.voided_at IS NULL
		             AND e.quantity_shares IS NOT NULL
		             AND e.trade_date < ca.ex_date
		       ), 0)::text
		FROM corporate_actions ca
		JOIN assets a ON a.id = ca.asset_id
		WHERE ca.action_type = 'cash_dividend'
		  AND ca.cash_per_share IS NOT NULL
		  AND ($2 = '' OR ca.asset_id = NULLIF($2, '')::uuid)
		  AND EXISTS (
			SELECT 1 FROM ledger_events e2
			WHERE e2.user_id = $1 AND e2.asset_id = ca.asset_id AND e2.voided_at IS NULL
		  )
		ORDER BY ca.ex_date, ca.id
	`, userID, assetID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var candidates []DividendCandidate
	for rows.Next() {
		var c DividendCandidate
		var exDateText, perShareText, heldText string
		var payDateText sql.NullString
		if err := rows.Scan(&c.ActionID, &c.AssetID, &c.Symbol, &exDateText, &payDateText, &perShareText, &heldText); err != nil {
			return nil, err
		}
		exDate, err := parseDateText(exDateText)
		if err != nil {
			return nil, err
		}
		c.ExDate = exDate
		if payDateText.Valid {
			payDate, err := parseDateText(payDateText.String)
			if err != nil {
				return nil, err
			}
			c.PayDate = &payDate
		}
		c.CashPerShare = num.Parse(perShareText)
		c.HeldShares = num.Parse(heldText)
		candidates = append(candidates, c)
	}
	return candidates, rows.Err()
}

// recordedDividends ledger 內已存在的未作廢 cash_dividend 事件。
func (s *Service) recordedDividends(ctx context.Context, userID, assetID string) ([]RecordedDividend, error) {
	rows, err := s.db.Query(ctx, `
		SELECT e.asset_id::text, e.trade_date::text
		FROM ledger_events e
		WHERE e.user_id = $1 AND e.voided_at IS NULL
		  AND e.event_type = 'cash_dividend' AND e.asset_id IS NOT NULL
		  AND ($2 = '' OR e.asset_id = NULLIF($2, '')::uuid)
	`, userID, assetID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var recorded []RecordedDividend
	for rows.Next() {
		var r RecordedDividend
		var dateText string
		if err := rows.Scan(&r.AssetID, &dateText); err != nil {
			return nil, err
		}
		tradeDate, err := parseDateText(dateText)
		if err != nil {
			return nil, err
		}
		r.TradeDate = tradeDate
		recorded = append(recorded, r)
	}
	return recorded, rows.Err()
}

// assetIDsBySymbol 把代號對應回 assets.id；同代號跨市場時取 market 排序最前者（與 ledger 規則一致）。
func (s *Service) assetIDsBySymbol(ctx context.Context, symbols []string) (map[string]string, error) {
	result := map[string]string{}
	if len(symbols) == 0 {
		return result, nil
	}
	rows, err := s.db.Query(ctx, `
		SELECT DISTINCT ON (symbol) symbol, id::text
		FROM assets
		WHERE symbol = ANY($1) AND is_active = true
		ORDER BY symbol, market
	`, symbols)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var symbol, id string
		if err := rows.Scan(&symbol, &id); err != nil {
			return nil, err
		}
		result[symbol] = id
	}
	return result, rows.Err()
}

func (s *Service) insertDiff(ctx context.Context, tx pgx.Tx, runID, assetID, diffType, internal, broker string) (Diff, error) {
	var diff Diff
	var internalVal any
	if internal != "" {
		internalVal = internal
	}
	var brokerVal any
	if broker != "" {
		brokerVal = broker
	}
	err := tx.QueryRow(ctx, `
		INSERT INTO reconciliation_diffs (run_id, asset_id, diff_type, internal_value, broker_value)
		VALUES ($1, NULLIF($2, '')::uuid, $3, $4, $5)
		RETURNING id::text, run_id::text, asset_id::text, diff_type, internal_value, broker_value, resolution
	`, runID, assetID, diffType, internalVal, brokerVal).
		Scan(&diff.ID, &diff.RunID, &diff.AssetID, &diff.DiffType, &diff.InternalValue, &diff.BrokerValue, &diff.Resolution)
	return diff, err
}

const diffSelectColumns = `
	d.id::text, d.run_id::text, d.asset_id::text, a.symbol, d.diff_type,
	d.internal_value, d.broker_value, d.resolution,
	d.adjustment_event_id::text, d.resolved_at::text
`

func (s *Service) diffs(ctx context.Context, runID string) ([]Diff, error) {
	rows, err := s.db.Query(ctx, `
		SELECT `+diffSelectColumns+`
		FROM reconciliation_diffs d
		LEFT JOIN assets a ON a.id = d.asset_id
		WHERE d.run_id = $1
		ORDER BY d.created_at, d.id
	`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var diffs []Diff
	for rows.Next() {
		diff, err := scanDiff(rows)
		if err != nil {
			return nil, err
		}
		diffs = append(diffs, diff)
	}
	return diffs, rows.Err()
}

func (s *Service) diff(ctx context.Context, diffID string) (Diff, error) {
	return scanDiff(s.db.QueryRow(ctx, `
		SELECT `+diffSelectColumns+`
		FROM reconciliation_diffs d
		LEFT JOIN assets a ON a.id = d.asset_id
		WHERE d.id = $1
	`, diffID))
}

// diffForUser 以使用者視角讀一筆差異，順便帶出所屬 run。
func (s *Service) diffForUser(ctx context.Context, userID, diffID string) (Diff, string, error) {
	var runID string
	diff, err := scanDiffWith(s.db.QueryRow(ctx, `
		SELECT `+diffSelectColumns+`, r.id::text
		FROM reconciliation_diffs d
		JOIN reconciliation_runs r ON r.id = d.run_id
		LEFT JOIN assets a ON a.id = d.asset_id
		WHERE r.user_id = $1 AND d.id = $2
	`, userID, diffID), &runID)
	return diff, runID, err
}

type scanner interface{ Scan(...any) error }

func scanDiff(row scanner) (Diff, error) {
	return scanDiffWith(row)
}

func scanDiffWith(row scanner, extra ...any) (Diff, error) {
	var diff Diff
	var assetID, symbol, internal, broker, adjustmentEventID, resolvedAt sql.NullString
	dest := []any{&diff.ID, &diff.RunID, &assetID, &symbol, &diff.DiffType, &internal, &broker, &diff.Resolution, &adjustmentEventID, &resolvedAt}
	dest = append(dest, extra...)
	if err := row.Scan(dest...); err != nil {
		return Diff{}, err
	}
	diff.AssetID = nullStringPtr(assetID)
	diff.Symbol = nullStringPtr(symbol)
	diff.InternalValue = nullStringPtr(internal)
	diff.BrokerValue = nullStringPtr(broker)
	diff.AdjustmentEventID = nullStringPtr(adjustmentEventID)
	diff.ResolvedAt = nullStringPtr(resolvedAt)
	return diff, nil
}

func nullStringPtr(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	return &value.String
}

// decodeDetails 解析 broker_positions.details；用 json.Number 避免金額經過 float64。
func decodeDetails(raw []byte) map[string]any {
	if len(raw) == 0 {
		return nil
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var m map[string]any
	if err := dec.Decode(&m); err != nil {
		return nil
	}
	return m
}

// detailsDecimal 依序嘗試多個鍵名，把 details 內的數字欄位轉成 decimal。
func detailsDecimal(details map[string]any, keys ...string) *decimal.Decimal {
	for _, key := range keys {
		value, ok := details[key]
		if !ok {
			continue
		}
		switch v := value.(type) {
		case string:
			cleaned := strings.ReplaceAll(strings.TrimSpace(v), ",", "")
			if cleaned == "" {
				continue
			}
			if d, err := decimal.NewFromString(cleaned); err == nil {
				return &d
			}
		case json.Number:
			if d, err := decimal.NewFromString(v.String()); err == nil {
				return &d
			}
		}
	}
	return nil
}

// parseDateText 解析 PostgreSQL DATE 轉文字後的 "YYYY-MM-DD"。
func parseDateText(value string) (time.Time, error) {
	t, err := time.Parse("2006-01-02", value)
	if err != nil {
		return time.Time{}, fmt.Errorf("日期格式不合法 %q：%w", value, err)
	}
	return t, nil
}

func IsNotFound(err error) bool {
	return errors.Is(err, pgx.ErrNoRows)
}
