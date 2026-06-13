import { market } from '$lib/api';
import type { FreshnessItem } from '$lib/api';

/*
  市場資料新鮮度：頂部 chip 與市場頁共用。
  以日終行情（daily_bars）為主要顯示依據。
*/
class FreshnessStore {
	items = $state<FreshnessItem[]>([]);
	loading = $state(false);
	loaded = $state(false);
	error = $state<unknown>(null);

	get primary(): FreshnessItem | null {
		return (
			this.items.find((i) => i.dataset === 'daily_bars') ??
			this.items.find((i) => i.dataset === 'daily_bars_backfill') ??
			this.items[0] ??
			null
		);
	}

	get marketDataAsOf(): string | null {
		return this.primary?.latest_data_date ?? null;
	}

	get staleDays(): number | null {
		return this.primary?.stale_trading_days ?? null;
	}

	async refresh(): Promise<void> {
		this.loading = true;
		this.error = null;
		try {
			this.items = await market.freshness();
			this.loaded = true;
		} catch (e) {
			this.error = e;
		} finally {
			this.loading = false;
		}
	}

	async ensure(): Promise<void> {
		if (!this.loaded && !this.loading) await this.refresh();
	}
}

export const freshness = new FreshnessStore();
