import Decimal from 'decimal.js';

/*
  金額與數量一律以字串在 API 間傳輸，前端用 decimal.js 運算，禁止轉 float 後回送。
  這裡集中所有顯示格式化與單位換算。
*/

export const TW_LOT_SIZE = 1000;

export function toDecimal(value: string | number | null | undefined): Decimal | null {
	if (value === null || value === undefined || value === '') return null;
	try {
		const d = new Decimal(value);
		return d.isFinite() ? d : null;
	} catch {
		return null;
	}
}

/** 金額：千分位、預設 0 位小數，可指定小數位 */
export function formatMoney(
	value: string | number | null | undefined,
	opts: { dp?: number; sign?: boolean } = {}
): string {
	const d = toDecimal(value);
	if (d === null) return '—';
	const dp = opts.dp ?? 0;
	const rounded = d.toDecimalPlaces(dp, Decimal.ROUND_HALF_UP);
	const fixed = rounded.toFixed(dp);
	const [intPart, decPart] = fixed.replace('-', '').split('.');
	const grouped = intPart.replace(/\B(?=(\d{3})+(?!\d))/g, ',');
	const body = decPart ? `${grouped}.${decPart}` : grouped;
	const neg = rounded.isNegative();
	if (opts.sign && !neg && !rounded.isZero()) return `+${body}`;
	return neg ? `-${body}` : body;
}

/** 百分比：輸入 0~1 的比例，輸出 % 字串 */
export function formatPercent(
	ratio: string | number | null | undefined,
	dp = 1,
	opts: { sign?: boolean } = {}
): string {
	const d = toDecimal(ratio);
	if (d === null) return '—';
	const pct = d.times(100).toDecimalPlaces(dp, Decimal.ROUND_HALF_UP);
	const body = pct.toFixed(dp);
	if (opts.sign && pct.greaterThan(0)) return `+${body}%`;
	return `${body}%`;
}

/** 由股數推張數字串（顯示用，整張不帶小數，零股顯示小數） */
export function sharesToLots(
	shares: string | number | null | undefined,
	lotSize = TW_LOT_SIZE
): string {
	const d = toDecimal(shares);
	if (d === null || lotSize <= 0) return '—';
	const lots = d.div(lotSize);
	if (lots.isInteger()) return lots.toFixed(0);
	return lots.toDecimalPlaces(3, Decimal.ROUND_DOWN).toString();
}

/** 數量輸入（quantity + unit）換算成股數字串，給 API */
export function unitToShares(
	quantity: string | number,
	unit: 'share' | 'lot',
	lotSize = TW_LOT_SIZE
): string | null {
	const d = toDecimal(quantity);
	if (d === null) return null;
	const shares = unit === 'lot' ? d.times(lotSize) : d;
	return shares.toString();
}

/** 股數整數格式化（千分位） */
export function formatShares(value: string | number | null | undefined): string {
	const d = toDecimal(value);
	if (d === null) return '—';
	return formatMoney(d.toString(), { dp: d.isInteger() ? 0 : 2 });
}

/** 損益方向：台股漲紅跌綠 */
export type Tone = 'up' | 'down' | 'flat';
export function toneOf(value: string | number | null | undefined): Tone {
	const d = toDecimal(value);
	if (d === null || d.isZero()) return 'flat';
	return d.isPositive() ? 'up' : 'down';
}

export function toneClass(tone: Tone): string {
	if (tone === 'up') return 'text-up';
	if (tone === 'down') return 'text-down';
	return 'text-flat';
}

/** 日期顯示：YYYY-MM-DD（保留字串原樣或截斷 timestamp） */
export function formatDate(value: string | null | undefined): string {
	if (!value) return '—';
	return value.slice(0, 10);
}

/** 相對時間（粗略） */
export function formatDateTime(value: string | null | undefined): string {
	if (!value) return '—';
	const d = new Date(value);
	if (isNaN(d.getTime())) return value;
	const pad = (n: number) => n.toString().padStart(2, '0');
	return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}`;
}

/** 產生 Idempotency-Key（寫入型 POST 用） */
export function newIdempotencyKey(): string {
	if (typeof crypto !== 'undefined' && 'randomUUID' in crypto) return crypto.randomUUID();
	return `idem-${Date.now()}-${Math.floor(Math.random() * 1e9)}`;
}
