<script lang="ts">
	import { onMount } from 'svelte';
	import { page } from '$app/state';
	import { goto } from '$app/navigation';
	import { portfolio as portfolioApi } from '$lib/api';
	import type { Lot } from '$lib/api';
	import { toDecimal, formatMoney, formatDate } from '$lib/format';

	import PageState from '$lib/components/app/page-state.svelte';
	import Quantity from '$lib/components/app/quantity.svelte';
	import * as Card from '$lib/components/ui/card';
	import * as Table from '$lib/components/ui/table';
	import { Button } from '$lib/components/ui/button';
	import { Input } from '$lib/components/ui/input';
	import { Label } from '$lib/components/ui/label';
	import { Badge } from '$lib/components/ui/badge';

	import LayersIcon from '@lucide/svelte/icons/layers';
	import PlusIcon from '@lucide/svelte/icons/plus';
	import SearchIcon from '@lucide/svelte/icons/search';
	import InfoIcon from '@lucide/svelte/icons/info';

	let lots = $state<Lot[]>([]);
	let loading = $state(true);
	let error = $state<unknown>(null);

	// 過濾用標的代號，初始值取自網址 query。
	let symbolFilter = $state(page.url.searchParams.get('symbol') ?? '');

	async function load(symbol?: string) {
		loading = true;
		error = null;
		try {
			const s = symbol?.trim() || undefined;
			lots = await portfolioApi.lots(s);
		} catch (e) {
			error = e;
		} finally {
			loading = false;
		}
	}

	onMount(() => load(symbolFilter));

	function applyFilter(e: Event) {
		e.preventDefault();
		const s = symbolFilter.trim();
		// 同步更新 URL（非必須，但對分享連結友善）。
		const url = new URL(page.url);
		if (s) url.searchParams.set('symbol', s);
		else url.searchParams.delete('symbol');
		goto(`${url.pathname}${url.search}`, { replaceState: true, keepFocus: true, noScroll: true });
		load(s);
	}

	function clearFilter() {
		symbolFilter = '';
		const url = new URL(page.url);
		url.searchParams.delete('symbol');
		goto(url.pathname, { replaceState: true, keepFocus: true, noScroll: true });
		load();
	}

	// 依 symbol 分組（保留 API 回傳順序）。
	const groups = $derived.by(() => {
		const map = new Map<string, Lot[]>();
		for (const lot of lots) {
			const arr = map.get(lot.symbol);
			if (arr) arr.push(lot);
			else map.set(lot.symbol, [lot]);
		}
		return Array.from(map, ([symbol, items]) => ({ symbol, items }));
	});

	/** 每股成本：cost / quantity，用 decimal.js 計算。 */
	function perShare(cost: string, quantity: string): string | null {
		const c = toDecimal(cost);
		const q = toDecimal(quantity);
		if (c === null || q === null || q.isZero()) return null;
		return c.div(q).toString();
	}

	function isClosed(lot: Lot): boolean {
		return Boolean(lot.closed_at);
	}
</script>

<div class="space-y-6">
	<!-- 頁首 -->
	<div class="flex flex-col gap-1">
		<h1 class="text-2xl font-semibold tracking-tight">批次明細</h1>
		<p class="text-muted-foreground text-sm">
			每筆買進形成一個批次，呈現原始成本與調整後成本兩種視角。調整後成本反映配息、減資等對成本的調整，供你與券商
			App 對帳。
		</p>
	</div>

	<!-- 過濾器 -->
	<Card.Root>
		<Card.Content class="py-4">
			<form class="flex flex-col gap-3 sm:flex-row sm:items-end" onsubmit={applyFilter}>
				<div class="flex-1 space-y-1.5">
					<Label for="symbol-filter">標的代號</Label>
					<Input
						id="symbol-filter"
						placeholder="輸入代號過濾，例如 2330（留空顯示全部）"
						bind:value={symbolFilter}
					/>
				</div>
				<div class="flex gap-2">
					<Button type="submit"><SearchIcon class="size-4" />套用過濾</Button>
					{#if symbolFilter}
						<Button type="button" variant="outline" onclick={clearFilter}>清除</Button>
					{/if}
				</div>
			</form>
		</Card.Content>
	</Card.Root>

	<PageState
		{loading}
		{error}
		empty={groups.length === 0}
		onRetry={() => load(symbolFilter)}
		emptyIcon={LayersIcon}
		emptyTitle="沒有批次資料"
		emptyHint={symbolFilter
			? `找不到「${symbolFilter}」的批次，請確認代號，或先到交易頁新增買進交易。`
			: '批次由買進交易產生。先到交易頁新增一筆買進，這裡就會列出對應的成本批次。'}
	>
		{#snippet emptyAction()}
			<Button href="/transactions"><PlusIcon class="size-4" />前往交易頁</Button>
		{/snippet}

		<div class="space-y-6">
			{#each groups as group (group.symbol)}
				<Card.Root>
					<Card.Header class="flex flex-row items-center justify-between">
						<div>
							<Card.Title>{group.symbol}</Card.Title>
							<Card.Description>共 {group.items.length} 個批次</Card.Description>
						</div>
						<Button variant="ghost" size="sm" href="/lots?symbol={group.symbol}">只看此標的</Button>
					</Card.Header>
					<Card.Content class="px-0">
						<div class="scrollbar-thin overflow-x-auto">
							<Table.Root>
								<Table.Header>
									<Table.Row>
										<Table.Head>開倉日</Table.Head>
										<Table.Head class="text-right">原始股數</Table.Head>
										<Table.Head class="text-right">剩餘股數</Table.Head>
										<Table.Head class="text-right">原始成本</Table.Head>
										<Table.Head class="text-right">每股原始成本</Table.Head>
										<Table.Head class="text-right">調整後成本</Table.Head>
										<Table.Head class="text-right">每股調整後成本</Table.Head>
										<Table.Head class="text-right">狀態</Table.Head>
									</Table.Row>
								</Table.Header>
								<Table.Body>
									{#each group.items as lot (lot.id)}
										{@const closed = isClosed(lot)}
										{@const origPer = perShare(lot.original_cost, lot.original_quantity)}
										{@const adjPer = perShare(lot.adjusted_cost, lot.original_quantity)}
										<Table.Row class={closed ? 'text-muted-foreground' : 'hover:bg-muted/40'}>
											<Table.Cell class="tnum whitespace-nowrap"
												>{formatDate(lot.open_date)}</Table.Cell
											>
											<Table.Cell class="text-right">
												<Quantity shares={lot.original_quantity} inline />
											</Table.Cell>
											<Table.Cell class="text-right">
												<Quantity shares={lot.remaining_quantity} inline />
											</Table.Cell>
											<Table.Cell class="tnum text-right"
												>{formatMoney(lot.original_cost)}</Table.Cell
											>
											<Table.Cell class="tnum text-right">
												{origPer ? formatMoney(origPer, { dp: 2 }) : '—'}
											</Table.Cell>
											<Table.Cell class="tnum text-right font-medium"
												>{formatMoney(lot.adjusted_cost)}</Table.Cell
											>
											<Table.Cell class="tnum text-right font-medium">
												{adjPer ? formatMoney(adjPer, { dp: 2 }) : '—'}
											</Table.Cell>
											<Table.Cell class="text-right">
												{#if closed}
													<Badge variant="outline" class="font-normal">已平倉</Badge>
												{:else}
													<Badge variant="secondary" class="font-normal">持有中</Badge>
												{/if}
											</Table.Cell>
										</Table.Row>
									{/each}
								</Table.Body>
							</Table.Root>
						</div>
					</Card.Content>
					<Card.Footer class="text-muted-foreground gap-2 text-xs">
						<InfoIcon class="size-3.5 shrink-0" />
						<span
							>左側「原始成本」為當初買進的實際成本；右側「調整後成本」已納入配息、減資等調整，便於與券商
							App 的庫存成本核對。</span
						>
					</Card.Footer>
				</Card.Root>
			{/each}
		</div>
	</PageState>
</div>
