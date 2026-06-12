package reconcile

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/tingz/easy-invest/internal/ledger"
	"github.com/tingz/easy-invest/internal/num"
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
	ID            string  `json:"id"`
	RunID         string  `json:"run_id"`
	AssetID       *string `json:"asset_id,omitempty"`
	Symbol        *string `json:"symbol,omitempty"`
	DiffType      string  `json:"diff_type"`
	InternalValue *string `json:"internal_value,omitempty"`
	BrokerValue   *string `json:"broker_value,omitempty"`
	Resolution    string  `json:"resolution"`
}

type Run struct {
	ID               string `json:"id"`
	BrokerSnapshotID string `json:"broker_snapshot_id"`
	Status           string `json:"status"`
	CreatedAt        string `json:"created_at"`
	Diffs            []Diff `json:"diffs,omitempty"`
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
	portfolio, err := s.ledger.Portfolio(ctx, userID)
	if err != nil {
		return Run{}, err
	}
	internalQty := map[string]string{}
	internalAssetID := map[string]string{}
	for _, p := range portfolio.Positions {
		internalQty[p.Symbol] = p.QuantityShares
		internalAssetID[p.Symbol] = p.AssetID
	}
	rows, err := tx.Query(ctx, `SELECT symbol, quantity_shares::text FROM broker_positions WHERE snapshot_id = $1`, snapshotID)
	if err != nil {
		return Run{}, err
	}
	defer rows.Close()
	seen := map[string]bool{}
	for rows.Next() {
		var symbol, brokerQty string
		if err := rows.Scan(&symbol, &brokerQty); err != nil {
			return Run{}, err
		}
		seen[symbol] = true
		internal := internalQty[symbol]
		if num.Parse(internal).Cmp(num.Parse(brokerQty)) != 0 {
			diff, err := s.insertDiff(ctx, tx, run.ID, internalAssetID[symbol], "quantity_mismatch", internal, brokerQty)
			if err != nil {
				return Run{}, err
			}
			run.Diffs = append(run.Diffs, diff)
		}
	}
	if err := rows.Err(); err != nil {
		return Run{}, err
	}
	for symbol, internal := range internalQty {
		if !seen[symbol] {
			diff, err := s.insertDiff(ctx, tx, run.ID, internalAssetID[symbol], "missing_position_broker", internal, "")
			if err != nil {
				return Run{}, err
			}
			run.Diffs = append(run.Diffs, diff)
		}
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

func (s *Service) ResolveDiff(ctx context.Context, userID, diffID, resolution string) (Diff, error) {
	if resolution != "accepted_as_is" && resolution != "fixed_input" && resolution != "adjusted" {
		return Diff{}, errors.New("invalid resolution")
	}
	tag, err := s.db.Exec(ctx, `
		UPDATE reconciliation_diffs d
		SET resolution = $3, resolved_at = now()
		FROM reconciliation_runs r
		WHERE d.run_id = r.id AND r.user_id = $1 AND d.id = $2
	`, userID, diffID, resolution)
	if err != nil {
		return Diff{}, err
	}
	if tag.RowsAffected() == 0 {
		return Diff{}, pgx.ErrNoRows
	}
	return s.diff(ctx, diffID)
}

func (s *Service) insertDiff(ctx context.Context, tx pgx.Tx, runID, assetID, diffType, internal, broker string) (Diff, error) {
	var diff Diff
	var asset any
	if assetID != "" {
		asset = assetID
	}
	var internalPtr any
	if internal != "" {
		internalPtr = internal
	}
	var brokerPtr any
	if broker != "" {
		brokerPtr = broker
	}
	err := tx.QueryRow(ctx, `
		INSERT INTO reconciliation_diffs (run_id, asset_id, diff_type, internal_value, broker_value)
		VALUES ($1, NULLIF($2, '')::uuid, $3, $4, $5)
		RETURNING id::text, run_id::text, asset_id::text, diff_type, internal_value, broker_value, resolution
	`, runID, stringOrEmpty(asset), diffType, internalPtr, brokerPtr).
		Scan(&diff.ID, &diff.RunID, &diff.AssetID, &diff.DiffType, &diff.InternalValue, &diff.BrokerValue, &diff.Resolution)
	return diff, err
}

func (s *Service) diffs(ctx context.Context, runID string) ([]Diff, error) {
	rows, err := s.db.Query(ctx, `
		SELECT d.id::text, d.run_id::text, d.asset_id::text, a.symbol, d.diff_type, d.internal_value, d.broker_value, d.resolution
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
		SELECT d.id::text, d.run_id::text, d.asset_id::text, a.symbol, d.diff_type, d.internal_value, d.broker_value, d.resolution
		FROM reconciliation_diffs d
		LEFT JOIN assets a ON a.id = d.asset_id
		WHERE d.id = $1
	`, diffID))
}

type scanner interface{ Scan(...any) error }

func scanDiff(row scanner) (Diff, error) {
	var diff Diff
	var assetID, symbol, internal, broker sql.NullString
	if err := row.Scan(&diff.ID, &diff.RunID, &assetID, &symbol, &diff.DiffType, &internal, &broker, &diff.Resolution); err != nil {
		return Diff{}, err
	}
	diff.AssetID = nullStringPtr(assetID)
	diff.Symbol = nullStringPtr(symbol)
	diff.InternalValue = nullStringPtr(internal)
	diff.BrokerValue = nullStringPtr(broker)
	return diff, nil
}

func nullStringPtr(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	return &value.String
}

func stringOrEmpty(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func IsNotFound(err error) bool {
	return errors.Is(err, pgx.ErrNoRows)
}
