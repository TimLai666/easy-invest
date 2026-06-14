import { api } from './client';
import { newIdempotencyKey } from '../format';
import type {
	ApiKey,
	ApiKeyWithSecret,
	Asset,
	BacktestRun,
	BrokerSnapshot,
	CalendarDay,
	CorporateAction,
	CreateEventRequest,
	DailyBar,
	FreshnessItem,
	LedgerEvent,
	MeResponse,
	Portfolio,
	PortfolioHistoryPoint,
	Lot,
	RecommendationItem,
	RecommendationItemStatus,
	RecommendationRun,
	ReconciliationRun,
	Resolution,
	Settings,
	Strategy,
	User
} from './types';

type Items<T> = { items: T[] };

export const auth = {
	register: (body: { email: string; password: string; display_name?: string }) =>
		api.post<User>('/auth/register', { body }),
	login: (body: { email: string; password: string }) => api.post<User>('/auth/login', { body }),
	logout: () => api.post<void>('/auth/logout'),
	me: () => api.get<MeResponse>('/me')
};

export const apiKeys = {
	list: () => api.get<Items<ApiKey>>('/api-keys').then((r) => r.items ?? []),
	create: (body: {
		name: string;
		description?: string;
		scopes: string[];
		expires_at?: string | null;
	}) => api.post<ApiKeyWithSecret>('/api-keys', { body }),
	revoke: (id: string) => api.post<void>(`/api-keys/${id}/revoke`),
	rotate: (id: string) => api.post<ApiKeyWithSecret>(`/api-keys/${id}/rotate`)
};

export const market = {
	assets: (query?: { query?: string; type?: string; limit?: number }) =>
		api.get<Items<Asset>>('/assets', { query }).then((r) => r.items ?? []),
	asset: (id: string) => api.get<Asset>(`/assets/${id}`),
	bars: (query: { symbol?: string; from?: string; to?: string; limit?: number }) =>
		api.get<Items<DailyBar>>('/market/bars', { query }).then((r) => r.items ?? []),
	corporateActions: (query?: { symbol?: string; from?: string; to?: string }) =>
		api
			.get<Items<CorporateAction>>('/market/corporate-actions', { query })
			.then((r) => r.items ?? []),
	calendar: (query?: { from?: string; to?: string }) =>
		api.get<Items<CalendarDay>>('/market/calendar', { query }).then((r) => r.items ?? []),
	freshness: () => api.get<Items<FreshnessItem>>('/market/freshness').then((r) => r.items ?? [])
};

export const ledger = {
	create: (body: CreateEventRequest) =>
		api.post<LedgerEvent>('/ledger/events', { body, idempotencyKey: newIdempotencyKey() }),
	list: (query?: { limit?: number; asset?: string; type?: string; from?: string; to?: string }) =>
		api.get<Items<LedgerEvent>>('/ledger/events', { query }).then((r) => r.items ?? []),
	get: (id: string) => api.get<LedgerEvent>(`/ledger/events/${id}`),
	void: (id: string, reason: string) =>
		api.post<void>(`/ledger/events/${id}/void`, {
			body: { reason },
			idempotencyKey: newIdempotencyKey()
		})
};

export const portfolio = {
	get: () => api.get<Portfolio>('/portfolio'),
	history: (query?: { from?: string; to?: string; limit?: number }) =>
		api.get<Items<PortfolioHistoryPoint>>('/portfolio/history', { query }).then((r) => r.items ?? []),
	lots: (symbol?: string) =>
		api.get<Items<Lot>>('/portfolio/lots', { query: { symbol } }).then((r) => r.items ?? [])
};

export const recommendations = {
	run: (target_weights?: Record<string, string>) =>
		api.post<RecommendationRun>('/recommendations/runs', {
			body: target_weights ? { target_weights } : {},
			idempotencyKey: newIdempotencyKey()
		}),
	list: (limit = 10) =>
		api
			.get<Items<RecommendationRun>>('/recommendations/runs', { query: { limit } })
			.then((r) => r.items ?? []),
	get: (id: string) => api.get<RecommendationRun>(`/recommendations/runs/${id}`),
	setItemStatus: (id: string, user_status: Exclude<RecommendationItemStatus, 'pending'>) =>
		api.patch<RecommendationItem>(`/recommendations/items/${id}`, { body: { user_status } })
};

export interface CreateSnapshotBody {
	broker_name: string;
	source?: string;
	captured_at?: string | null;
	positions: {
		symbol: string;
		quantity_shares: string;
		broker_avg_cost?: string | null;
		details?: Record<string, unknown>;
	}[];
	raw_payload?: Record<string, unknown>;
}

export const reconciliation = {
	createSnapshot: (body: CreateSnapshotBody) =>
		api.post<BrokerSnapshot>('/reconciliation/broker-snapshots', {
			body,
			idempotencyKey: newIdempotencyKey()
		}),
	listSnapshots: () =>
		api.get<Items<BrokerSnapshot>>('/reconciliation/broker-snapshots').then((r) => r.items ?? []),
	createRun: (broker_snapshot_id: string) =>
		api.post<ReconciliationRun>('/reconciliation/runs', {
			body: { broker_snapshot_id },
			idempotencyKey: newIdempotencyKey()
		}),
	getRun: (id: string) => api.get<ReconciliationRun>(`/reconciliation/runs/${id}`),
	resolveDiff: (id: string, resolution: Exclude<Resolution, 'pending'>) =>
		api.post<import('./types').ReconciliationDiff>(`/reconciliation/diffs/${id}/resolve`, {
			body: { resolution },
			idempotencyKey: newIdempotencyKey()
		})
};

export interface BacktestRunBody {
	name?: string;
	symbols?: string[];
	from?: string;
	to?: string;
	initial_cash: string;
	target_weights?: Record<string, string>;
	benchmark_symbol?: string;
	monthly_amount?: string;
}

export interface WalkForwardBody {
	symbols?: string[];
	from?: string;
	to?: string;
	initial_cash: string;
	target_weights?: Record<string, string>;
	bands?: string[];
	train_months?: number;
	test_months?: number;
	step_months?: number;
	slippage_bps?: string;
	objective?: 'sharpe' | 'return';
}

export const backtests = {
	run: (body: BacktestRunBody) =>
		api.post<BacktestRun>('/backtests/runs', { body, idempotencyKey: newIdempotencyKey() }),
	list: (limit = 20) =>
		api.get<Items<BacktestRun>>('/backtests/runs', { query: { limit } }).then((r) => r.items ?? []),
	get: (id: string) => api.get<BacktestRun>(`/backtests/runs/${id}`),
	walkForward: (body: WalkForwardBody) =>
		api.post<import('./types').WalkForwardReport>('/backtests/walk-forward', {
			body,
			idempotencyKey: newIdempotencyKey()
		})
};

export const settings = {
	get: () => api.get<Settings>('/settings'),
	update: (body: Record<string, unknown>) => api.put<Settings>('/settings', { body }),
	strategies: () => api.get<Items<Strategy>>('/strategies').then((r) => r.items ?? [])
};

export const system = {
	health: () =>
		api.get<{ status: string; db: string; migration_version: number; migration_dirty: boolean }>(
			'/healthz'
		),
	version: () => api.get<{ version: string }>('/version')
};
