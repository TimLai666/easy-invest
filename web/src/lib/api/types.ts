/*
  與後端 /api/v1 對應的型別。金額/數量欄位皆為 decimal 字串。
*/

export type Unit = 'share' | 'lot';

export type AssetType = 'tw_stock' | 'tw_etf' | 'tw_bond_etf';

export type EventType =
	| 'buy'
	| 'sell'
	| 'cash_dividend'
	| 'stock_dividend'
	| 'split'
	| 'capital_reduction'
	| 'cash_deposit'
	| 'cash_withdraw'
	| 'fee_adjustment'
	| 'tax_adjustment'
	| 'broker_reconciliation_adjustment'
	| 'manual_correction';

export type RecommendationAction = 'buy' | 'sell' | 'hold' | 'no_action';
export type RecommendationItemStatus = 'pending' | 'viewed' | 'accepted' | 'ignored' | 'expired';

export type DiffType =
	| 'quantity_mismatch'
	| 'avg_cost_mismatch'
	| 'missing_dividend'
	| 'missing_position_internal'
	| 'missing_position_broker'
	| 'fee_tax_mismatch';

export type Resolution = 'pending' | 'adjusted' | 'accepted_as_is' | 'fixed_input';

export type BacktestStatus = 'pending' | 'completed' | 'failed';

export type Scope =
	| 'market:read'
	| 'ledger:read'
	| 'ledger:write'
	| 'recommendations:read'
	| 'recommendations:run'
	| 'reconciliation:read'
	| 'reconciliation:write'
	| 'backtests:read'
	| 'backtests:run'
	| 'settings:read'
	| 'settings:write';

export interface User {
	id: string;
	email: string;
	display_name: string;
	created_at: string;
}

export interface MeResponse {
	user: User;
	auth_via: 'session' | 'api_key';
	scopes: string[] | null;
}

export interface ApiKey {
	id: string;
	name: string;
	description: string;
	key_prefix: string;
	scopes: string[];
	created_at: string;
	expires_at?: string;
	last_used_at?: string;
	revoked_at?: string;
}

export interface ApiKeyWithSecret extends ApiKey {
	plaintext: string;
}

export interface Asset {
	id: string;
	asset_type: AssetType;
	symbol: string;
	name: string;
	market: string;
	currency: string;
	lot_size: number;
	is_active: boolean;
	metadata?: Record<string, unknown>;
}

export interface Position {
	asset_id: string;
	symbol: string;
	name: string;
	asset_type: AssetType;
	market: string;
	currency: string;
	lot_size: number;
	quantity_shares: string;
	quantity_lots: string;
	original_cost: string;
	adjusted_cost: string;
	average_cost: string;
	adjusted_average_cost: string;
	market_price?: string;
	market_value?: string;
	unrealized_pnl?: string;
	market_data_as_of?: string;
}

export interface Portfolio {
	user_id: string;
	as_of: string;
	cash: string;
	market_data_as_of?: string;
	positions: Position[];
}

export interface PortfolioHistoryPoint {
	date: string;
	cash: string;
	market_value: string;
	total_value: string;
	market_data_as_of?: string;
	missing_price_symbols?: string[];
	stale_price_symbols?: string[];
	is_complete: boolean;
}

export interface Lot {
	id: string;
	asset_id: string;
	symbol: string;
	opened_by_event_id: string;
	open_date: string;
	original_quantity: string;
	remaining_quantity: string;
	original_cost: string;
	adjusted_cost: string;
	closed_at?: string;
}

export interface LedgerEvent {
	id: string;
	asset_id?: string;
	symbol?: string;
	event_type: EventType;
	trade_date: string;
	settlement_date?: string;
	quantity_shares?: string;
	quantity_lots?: string;
	price?: string;
	gross_amount?: string;
	fee: string;
	tax: string;
	cash_delta: string;
	currency: string;
	fee_source: 'user' | 'estimated';
	source: string;
	source_ref?: string;
	notes: string;
	metadata?: Record<string, unknown>;
	created_at: string;
	voided_at?: string;
	void_reason?: string;
}

export interface CreateEventRequest {
	event_type: EventType;
	symbol?: string;
	trade_date: string;
	settlement_date?: string | null;
	quantity?: string;
	unit?: Unit;
	price?: string;
	gross_amount?: string | null;
	fee?: string | null;
	tax?: string | null;
	source?: string;
	source_ref?: string | null;
	notes?: string;
	metadata?: Record<string, unknown>;
}

export interface DailyBar {
	id: string;
	asset_id: string;
	symbol: string;
	bar_date: string;
	open?: string;
	high?: string;
	low?: string;
	close: string;
	volume_shares?: string;
	turnover?: string;
	revision: number;
}

export interface CorporateAction {
	id: string;
	symbol: string;
	action_type: string;
	ex_date: string;
	record_date?: string | null;
	pay_date?: string | null;
	cash_per_share?: string | null;
	stock_ratio?: string | null;
	details: Record<string, unknown>;
}

export interface CalendarDay {
	market: string;
	date: string;
	is_open: boolean;
	note: string;
}

export interface FreshnessItem {
	dataset: string;
	latest_data_time: string;
	latest_fetched_at: string;
	source_name: string;
	status: string;
	latest_data_date?: string | null;
	stale_trading_days?: number | null;
}

export interface RecommendationItem {
	id: string;
	run_id?: string;
	asset_id?: string;
	symbol?: string;
	action: RecommendationAction;
	quantity_shares?: string;
	est_price?: string;
	est_amount?: string;
	est_fee_tax?: string;
	current_weight?: string;
	target_weight?: string;
	reason: string;
	risks: string;
	confidence?: string;
	user_status: RecommendationItemStatus;
}

export interface RecommendationRun {
	id: string;
	strategy: { name: string; version: string };
	market_data_as_of: string;
	portfolio_as_of: string;
	disclaimer: string;
	status: string;
	created_at: string;
	items?: RecommendationItem[];
}

export interface BrokerSnapshot {
	id: string;
	broker_name: string;
	source: string;
	captured_at: string;
	created_at: string;
}

export interface ReconciliationDiff {
	id: string;
	run_id: string;
	asset_id?: string;
	symbol?: string;
	diff_type: DiffType;
	internal_value?: string;
	broker_value?: string;
	resolution: Resolution;
	adjustment_event_id?: string;
	resolved_at?: string;
}

export interface ReconciliationRun {
	id: string;
	broker_snapshot_id: string;
	status: 'open' | 'resolved';
	created_at: string;
	diffs?: ReconciliationDiff[];
}

export interface BacktestResultDetail {
	initial_equity: string;
	final_equity: string;
	total_return: string;
	annualized_return: string;
	max_drawdown: string;
	final_cash: string;
	final_positions: Record<string, string>;
	equity_curve: { date: string; equity: string }[];
	trades: {
		date: string;
		symbol: string;
		action: string;
		quantity_shares: string;
		price: string;
		gross_amount: string;
		fee: string;
		tax: string;
		cash_delta: string;
	}[];
}

export interface BacktestResult {
	strategy: BacktestResultDetail;
	assumptions: string;
	benchmark_note?: string;
	benchmark_buy_hold?: BacktestResultDetail;
	benchmark_dca?: BacktestResultDetail;
	benchmark_dca_monthly_amount?: string;
}

export interface BacktestRun {
	id: string;
	name: string;
	status: BacktestStatus;
	params: Record<string, unknown>;
	result?: BacktestResult | null;
	error_message?: string;
	created_at: string;
}

export interface Settings {
	fee_rate: string;
	fee_discount: string;
	fee_minimum: string;
	dividend_transfer_fee: string;
	cash_buffer: string;
	min_trade_amount: string;
	prefer_whole_lot: boolean;
	risk_profile: string;
	target_weights: Record<string, string | number>;
	rebalance_band: string;
	updated_at: string;
}

export interface Strategy {
	id: string;
	name: string;
	version: string;
	description: string;
	created_at: number | string;
}

export interface ErrorDetail {
	field?: string;
	issue: string;
}

export interface ApiErrorBody {
	error: {
		code: string;
		message: string;
		details?: ErrorDetail[];
	};
}
