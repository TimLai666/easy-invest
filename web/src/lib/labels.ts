import type {
	EventType,
	RecommendationAction,
	RecommendationItemStatus,
	DiffType,
	Resolution,
	AssetType,
	Scope,
	BacktestStatus
} from '$lib/api';

export const eventTypeLabels: Record<EventType, string> = {
	buy: '買進',
	sell: '賣出',
	cash_dividend: '現金股利',
	stock_dividend: '股票股利',
	split: '股票分割',
	capital_reduction: '減資',
	cash_deposit: '現金存入',
	cash_withdraw: '現金提出',
	fee_adjustment: '手續費調整',
	tax_adjustment: '稅額調整',
	broker_reconciliation_adjustment: '對帳調整',
	manual_correction: '手動更正'
};

/** 使用者新增事件時可選的類型（不含系統/對帳自動產生者） */
export const userEventTypes: EventType[] = [
	'buy',
	'sell',
	'cash_dividend',
	'stock_dividend',
	'split',
	'capital_reduction',
	'cash_deposit',
	'cash_withdraw',
	'manual_correction'
];

export const actionLabels: Record<RecommendationAction, string> = {
	buy: '買進',
	sell: '賣出',
	hold: '持有',
	no_action: '無需調整'
};

export const itemStatusLabels: Record<RecommendationItemStatus, string> = {
	pending: '待檢視',
	viewed: '已檢視',
	accepted: '已採納',
	ignored: '已忽略',
	expired: '已過期'
};

export const diffTypeLabels: Record<DiffType, string> = {
	quantity_mismatch: '股數不符',
	avg_cost_mismatch: '均價不符',
	missing_dividend: '疑似漏記股利',
	missing_position_internal: '系統缺部位',
	missing_position_broker: '券商缺部位',
	fee_tax_mismatch: '費稅不符'
};

export const resolutionLabels: Record<Resolution, string> = {
	pending: '待處理',
	adjusted: '已建調整事件',
	accepted_as_is: '接受差異',
	fixed_input: '已修正輸入'
};

export const assetTypeLabels: Record<AssetType, string> = {
	tw_stock: '上市櫃股票',
	tw_etf: 'ETF',
	tw_bond_etf: '債券 ETF'
};

export const backtestStatusLabels: Record<BacktestStatus, string> = {
	pending: '計算中',
	completed: '完成',
	failed: '失敗'
};

export const scopeLabels: Record<Scope, string> = {
	'market:read': '市場資料（讀）',
	'ledger:read': '交易流水（讀）',
	'ledger:write': '交易流水（寫）',
	'recommendations:read': '建議（讀）',
	'recommendations:run': '建議（產生）',
	'reconciliation:read': '對帳（讀）',
	'reconciliation:write': '對帳（寫）',
	'backtests:read': '回測（讀）',
	'backtests:run': '回測（執行）',
	'settings:read': '設定（讀）',
	'settings:write': '設定（寫）'
};

export const allScopes: Scope[] = Object.keys(scopeLabels) as Scope[];

export const datasetLabels: Record<string, string> = {
	daily_bars: '上市日終行情',
	daily_bars_backfill: '歷史行情回補',
	corporate_actions: '公司行動',
	trading_calendar: '交易日曆',
	securities_list: '證券清單'
};

export function datasetLabel(name: string): string {
	return datasetLabels[name] ?? name;
}

export const riskProfileLabels: Record<string, string> = {
	conservative: '保守',
	balanced: '穩健',
	aggressive: '積極'
};
