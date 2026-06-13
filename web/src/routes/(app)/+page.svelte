<script lang="ts">
	import { onMount } from 'svelte';
	import { portfolio as portfolioApi, ApiError } from '$lib/api';
	import type { Portfolio } from '$lib/api';
	import { toDecimal, formatMoney } from '$lib/format';
	import { chartPalette } from '$lib/nav';
	import Decimal from 'decimal.js';

	import PageState from '$lib/components/app/page-state.svelte';
	import StatCard from '$lib/components/app/stat-card.svelte';
	import Money from '$lib/components/ui/money/money.svelte';
	import Pnl from '$lib/components/app/pnl.svelte';
	import Quantity from '$lib/components/app/quantity.svelte';
	import DonutChart, { type DonutSlice } from '$lib/components/app/donut-chart.svelte';
	import * as Card from '$lib/components/ui/card';
	import * as Table from '$lib/components/ui/table';
	import { Button } from '$lib/components/ui/button';
	import { Badge } from '$lib/components/ui/badge';

	import WalletIcon from '@lucide/svelte/icons/wallet';
	import CoinsIcon from '@lucide/svelte/icons/coins';
	import TrendingUpIcon from '@lucide/svelte/icons/trending-up';
	import PiggyBankIcon from '@lucide/svelte/icons/piggy-bank';
	import PlusIcon from '@lucide/svelte/icons/plus';
	import { assetTypeLabels } from '$lib/labels';

	let data = $state<Portfolio | null>(null);
	let loading = $state(true);
	let error = $state<unknown>(null);

	async function load() {
		loading = true;
		error = null;
		try {
			data = await portfolioApi.get();
		} catch (e) {
			error = e;
		} finally {
			loading = false;
		}
	}
	onMount(load);

	const positions = $derived(data?.positions ?? []);

	const totalMarketValue = $derived(
		positions.reduce((s, p) => s.plus(toDecimal(p.market_value) ?? new Decimal(0)), new Decimal(0))
	);
	const totalCost = $derived(
		positions.reduce((s, p) => s.plus(toDecimal(p.original_cost) ?? new Decimal(0)), new Decimal(0))
	);
	const totalPnl = $derived(
		positions.reduce(
			(s, p) => s.plus(toDecimal(p.unrealized_pnl) ?? new Decimal(0)),
			new Decimal(0)
		)
	);
	const cash = $derived(toDecimal(data?.cash) ?? new Decimal(0));
	const totalAssets = $derived(totalMarketValue.plus(cash));
	const pnlPct = $derived(totalCost.isZero() ? null : totalPnl.div(totalCost).toString());

	const slices = $derived.by<DonutSlice[]>(() => {
		const arr: DonutSlice[] = positions
			.map((p, i) => ({
				label: p.symbol,
				value: Number(toDecimal(p.market_value)?.toString() ?? 0),
				color: chartPalette[i % chartPalette.length]
			}))
			.filter((s) => s.value > 0);
		if (cash.greaterThan(0))
			arr.push({ label: '現金', value: Number(cash.toString()), color: 'var(--muted-foreground)' });
		return arr;
	});

	function weightOf(p: Portfolio['positions'][number]): number {
		const mv = toDecimal(p.market_value) ?? new Decimal(0);
		return totalAssets.isZero() ? 0 : mv.div(totalAssets).times(100).toNumber();
	}
</script>

<div class="space-y-6">
	<PageState
		{loading}
		{error}
		empty={positions.length === 0 && cash.isZero()}
		onRetry={load}
		emptyIcon={WalletIcon}
		emptyTitle="還沒有任何庫存"
		emptyHint="新增第一筆交易後，這裡會自動由交易流水計算出你的持股、成本與損益。"
	>
		{#snippet emptyAction()}
			<Button href="/transactions"><PlusIcon class="size-4" />新增第一筆交易</Button>
		{/snippet}

		<!-- 總覽卡片 -->
		<div class="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
			<StatCard label="總資產" icon={WalletIcon} accent>
				{#snippet value()}<Money value={totalAssets.toString()} currency />{/snippet}
				{#snippet footer()}持股市值 + 現金{/snippet}
			</StatCard>
			<StatCard label="持股市值" icon={CoinsIcon}>
				{#snippet value()}<Money value={totalMarketValue.toString()} />{/snippet}
				{#snippet footer()}
					{#if data?.market_data_as_of}依據 {data.market_data_as_of} 收盤{:else}尚無市場行情{/if}
				{/snippet}
			</StatCard>
			<StatCard label="未實現損益" icon={TrendingUpIcon}>
				{#snippet value()}<Pnl value={totalPnl.toString()} percent={pnlPct} />{/snippet}
				{#snippet footer()}成本 {formatMoney(totalCost.toString())}{/snippet}
			</StatCard>
			<StatCard label="可用現金" icon={PiggyBankIcon}>
				{#snippet value()}<Money value={cash.toString()} />{/snippet}
				{#snippet footer()}TWD{/snippet}
			</StatCard>
		</div>

		<div class="grid gap-6 lg:grid-cols-3">
			<!-- 配置圓環 -->
			<Card.Root class="lg:col-span-1">
				<Card.Header>
					<Card.Title>資產配置</Card.Title>
					<Card.Description>依目前市值計算的權重分布</Card.Description>
				</Card.Header>
				<Card.Content>
					{#if slices.length}
						<DonutChart
							data={slices}
							centerValue={formatMoney(totalAssets.toString())}
							centerLabel="總資產"
						/>
					{:else}
						<p class="text-muted-foreground py-8 text-center text-sm">尚無市值資料</p>
					{/if}
				</Card.Content>
			</Card.Root>

			<!-- 持股表 -->
			<Card.Root class="lg:col-span-2">
				<Card.Header class="flex flex-row items-center justify-between">
					<div>
						<Card.Title>持股明細</Card.Title>
						<Card.Description>共 {positions.length} 檔標的</Card.Description>
					</div>
					<Button variant="outline" size="sm" href="/transactions"
						><PlusIcon class="size-4" />新增交易</Button
					>
				</Card.Header>
				<Card.Content class="px-0">
					<div class="scrollbar-thin overflow-x-auto">
						<Table.Root>
							<Table.Header>
								<Table.Row>
									<Table.Head>標的</Table.Head>
									<Table.Head class="text-right">股數</Table.Head>
									<Table.Head class="text-right">均價</Table.Head>
									<Table.Head class="text-right">市價</Table.Head>
									<Table.Head class="text-right">市值</Table.Head>
									<Table.Head class="text-right">未實現損益</Table.Head>
									<Table.Head class="text-right">權重</Table.Head>
								</Table.Row>
							</Table.Header>
							<Table.Body>
								{#each positions as p (p.asset_id)}
									<Table.Row class="hover:bg-muted/40">
										<Table.Cell>
											<a href="/lots?symbol={p.symbol}" class="flex flex-col">
												<span class="font-medium">{p.symbol}</span>
												<span class="text-muted-foreground text-xs">{p.name}</span>
											</a>
										</Table.Cell>
										<Table.Cell class="text-right"
											><Quantity shares={p.quantity_shares} lots={p.quantity_lots} /></Table.Cell
										>
										<Table.Cell class="tnum text-right"
											>{formatMoney(p.average_cost, { dp: 2 })}</Table.Cell
										>
										<Table.Cell class="tnum text-right"
											>{p.market_price ? formatMoney(p.market_price, { dp: 2 }) : '—'}</Table.Cell
										>
										<Table.Cell class="tnum text-right"
											>{p.market_value ? formatMoney(p.market_value) : '—'}</Table.Cell
										>
										<Table.Cell class="text-right">
											{#if p.unrealized_pnl}<Pnl value={p.unrealized_pnl} />{:else}—{/if}
										</Table.Cell>
										<Table.Cell class="text-right">
											<Badge variant="secondary" class="tnum font-normal"
												>{weightOf(p).toFixed(1)}%</Badge
											>
										</Table.Cell>
									</Table.Row>
								{/each}
							</Table.Body>
						</Table.Root>
					</div>
				</Card.Content>
			</Card.Root>
		</div>
	</PageState>
</div>
