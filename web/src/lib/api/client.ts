import type { ApiErrorBody, ErrorDetail } from './types';

/** 後端固定錯誤信封對應的例外 */
export class ApiError extends Error {
	code: string;
	status: number;
	details?: ErrorDetail[];
	replay: boolean;

	constructor(status: number, body: ApiErrorBody | null, replay = false) {
		const message = body?.error?.message ?? `API 回應 ${status}`;
		super(message);
		this.name = 'ApiError';
		this.status = status;
		this.code = body?.error?.code ?? (status === 0 ? 'network' : 'internal');
		this.details = body?.error?.details;
		this.replay = replay;
	}

	/** 取得某欄位的錯誤訊息（表單欄位對應） */
	fieldIssue(field: string): string | undefined {
		return this.details?.find((d) => d.field === field)?.issue;
	}

	get isAuth(): boolean {
		return this.code === 'unauthorized' || this.status === 401;
	}
}

const BASE = '/api/v1';

export interface RequestOptions {
	query?: Record<string, string | number | boolean | undefined | null>;
	body?: unknown;
	idempotencyKey?: string;
	signal?: AbortSignal;
}

function buildUrl(path: string, query?: RequestOptions['query']): string {
	const url = new URL(BASE + path, window.location.origin);
	if (query) {
		for (const [k, v] of Object.entries(query)) {
			if (v !== undefined && v !== null && v !== '') url.searchParams.set(k, String(v));
		}
	}
	return url.pathname + url.search;
}

async function request<T>(method: string, path: string, opts: RequestOptions = {}): Promise<T> {
	const headers: Record<string, string> = {};
	let bodyInit: BodyInit | undefined;

	if (opts.body !== undefined) {
		headers['Content-Type'] = 'application/json';
		bodyInit = JSON.stringify(opts.body);
	}
	if (opts.idempotencyKey) headers['Idempotency-Key'] = opts.idempotencyKey;

	let res: Response;
	try {
		res = await fetch(buildUrl(path, opts.query), {
			method,
			headers,
			body: bodyInit,
			credentials: 'include',
			signal: opts.signal
		});
	} catch (e) {
		if ((e as Error)?.name === 'AbortError') throw e;
		throw new ApiError(0, {
			error: { code: 'network', message: '無法連線到伺服器，請確認後端是否啟動。' }
		});
	}

	const replay = res.headers.get('Idempotency-Replay') === 'true';

	if (res.status === 204) return undefined as T;

	const text = await res.text();
	let parsed: unknown = undefined;
	if (text) {
		try {
			parsed = JSON.parse(text);
		} catch {
			parsed = undefined;
		}
	}

	if (!res.ok) {
		throw new ApiError(res.status, (parsed as ApiErrorBody) ?? null, replay);
	}
	return parsed as T;
}

export const api = {
	get: <T>(path: string, opts?: RequestOptions) => request<T>('GET', path, opts),
	post: <T>(path: string, opts?: RequestOptions) => request<T>('POST', path, opts),
	put: <T>(path: string, opts?: RequestOptions) => request<T>('PUT', path, opts),
	patch: <T>(path: string, opts?: RequestOptions) => request<T>('PATCH', path, opts),
	del: <T>(path: string, opts?: RequestOptions) => request<T>('DELETE', path, opts)
};
