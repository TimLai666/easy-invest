<script lang="ts">
	import { onMount } from 'svelte';
	import { page } from '$app/state';
	import { recommendations as recApi, ApiError } from '$lib/api';
	import type { RecommendationRun, RecommendationItem, RecommendationItemStatus } from '$lib/api';
	import { formatMoney, formatPercent, formatDateTime } from '$lib/format';
	import { actionLabels, itemStatusLabels } from '$lib/labels';
	import { cn } from '$lib/utils';

	import PageState from '$lib/components/app/page-state.svelte';
	import Quantity from '$lib/components/app/quantity.svelte';
	import WeightBar from '$lib/components/app/weight-bar.svelte';
	import Disclaimer from '$lib/components/app/disclaimer.svelte';
	import * as Card from '$lib/components/ui/card';
	import { Button } from '$lib/components/ui/button';
	import { Badge } from '$lib/components/ui/badge';
	import { toast } from '$lib/components/ui/sonner';

	import ArrowLeftIcon from '@lucide/svelte/icons/arrow-left';
	import LightbulbIcon from '@lucide/svelte/icons/lightbulb';
	import CheckIcon from '@lucide/svelte/icons/check';
	import XIcon from '@lucide/svelte/icons/x';
	import CalendarIcon from '@lucide/svelte/icons/calendar';
	import ClockIcon from '@lucide/svelte/icons/clock';

	let data = $state<RecommendationRun | null>(null);
	let loading = $state(true);
	let error = $state<unknown>(null);
	let busyItemId = $state<string | null>(null);

	const id = $derived(page.params.id ?? '');

	async function load() {
		loading = true;
		error = null;
		try {
			data = await recApi.get(id);
		} catch (e) {
			error = e;
		} finally {
			loading = false;
		}
	}
	onMount(load);

	const items = $derived(data?.items ?? []);

	// action Badge 配色：買進紅（漲色）、賣出綠（跌色）、其餘中性
	function actionClass(action: RecommendationItem['action']): string {
		if (action === 'buy') return 'bg-up/10 text-up border-up/30';
		if (action === 'sell') return 'bg-down/10 text-down border-down/30';
		return 'bg-muted text-muted-foreground border-transparent';
	}

	function statusClass(status: RecommendationItemStatus): string {
		if (status === 'accepted') return 'bg-up/10 text-up border-up/30';
		if (status === 'ignored') return 'bg-muted text-muted-foreground border-transparent';
		return 'bg-muted text-muted-foreground border-transparent';
	}

	async function setStatus(item: RecommendationItem, user_status: 'accepted' | 'ignored') {
		if (busyItemId) return;
		busyItemId = item.id;
		const prev = item.user_status;
		// 樂觀更新
		item.user_status = user_status;
		try {
			await recApi.setItemStatus(item.id, user_status);
			toast.success(user_status === 'accepted' ? '已採納此建議' : '已忽略此建議');
		} catch (err) {
			item.user_status = prev;
			toast.error(err instanceof ApiError ? err.message : '操作失敗，請稍後再試');
		} finally {
			busyItemId = null;
		}
	}
</script>

<div class="space-y-6">
	<div>
		<Button variant="ghost" size="sm" href="/recommendations" class="text-muted-foreground -ml-2">
			<ArrowLeftIcon class="size-4" />返回建議列表
		</Button>
	</div>

	<PageState
		{loading}
		{error}
		empty={!!data && items.length === 0}
		onRetry={load}
		emptyIcon={LightbulbIcon}
		emptyTitle="這次建議沒有任何項目"
		emptyHint="目前的庫存與目標權重之間沒有需要調整的部位，或這次計算未產生建議。"
	>
		{#snippet emptyAction()}
			<Button href="/recommendations">回到建議列表</Button>
		{/snippet}

		{#if data}
			<!-- 摘要 -->
			<Card.Root>
				<Card.Header>
					<div class="flex flex-wrap items-start justify-between gap-3">
						<div class="space-y-1">
							<Card.Title class="flex items-center gap-2">
								<LightbulbIcon class="text-primary size-5" />
								{data.strategy.name}
								<Badge variant="secondary" class="font-normal">v{data.strategy.version}</Badge>
							</Card.Title>
							<Card.Description>共 {items.length} 項建議</Card.Description>
						</div>
						<div class="text-muted-foreground grid gap-1 text-xs sm:text-right">
							<span class="inline-flex items-center gap-1.5 sm:justify-end">
								<CalendarIcon class="size-3.5" />市場資料：{data.market_data_as_of}
							</span>
							<span class="inline-flex items-center gap-1.5 sm:justify-end">
								<CalendarIcon class="size-3.5" />庫存截至：{data.portfolio_as_of}
							</span>
							<span class="inline-flex items-center gap-1.5 sm:justify-end">
								<ClockIcon class="size-3.5" />建立於 {formatDateTime(data.created_at)}
							</span>
						</div>
					</div>
				</Card.Header>
			</Card.Root>

			<!-- items -->
			<div class="space-y-4">
				{#each items as item (item.id)}
					{@const resolved = item.user_status === 'accepted' || item.user_status === 'ignored'}
					<Card.Root>
						<Card.Content class="grid gap-6 p-5 lg:grid-cols-[1fr_minmax(18rem,22rem)]">
							<!-- 左區：行動、標的、理由、風險 -->
							<div class="space-y-3">
								<div class="flex flex-wrap items-center gap-2">
									<Badge variant="outline" class={cn('font-medium', actionClass(item.action))}>
										{actionLabels[item.action]}
									</Badge>
									<span class="text-base font-semibold">{item.symbol ?? '—'}</span>
									{#if resolved}
										<Badge
											variant="outline"
											class={cn('ml-auto font-normal', statusClass(item.user_status))}
										>
											{itemStatusLabels[item.user_status]}
										</Badge>
									{/if}
								</div>

								{#if item.reason}
									<p class="text-sm leading-relaxed">{item.reason}</p>
								{/if}
								{#if item.risks}
									<p class="text-muted-foreground text-xs leading-relaxed">風險：{item.risks}</p>
								{/if}

								<!-- 採納 / 忽略 -->
								<div class="flex gap-2 pt-1">
									<Button
										variant={item.user_status === 'accepted' ? 'default' : 'outline'}
										size="sm"
										disabled={busyItemId === item.id}
										onclick={() => setStatus(item, 'accepted')}
									>
										<CheckIcon class="size-4" />採納
									</Button>
									<Button
										variant={item.user_status === 'ignored' ? 'secondary' : 'outline'}
										size="sm"
										disabled={busyItemId === item.id}
										onclick={() => setStatus(item, 'ignored')}
									>
										<XIcon class="size-4" />忽略
									</Button>
								</div>
							</div>

							<!-- 右區：數量、估價/金額/費稅、權重、信心 -->
							<div class="bg-muted/30 space-y-3 rounded-lg p-4 text-sm">
								{#if item.quantity_shares}
									<div class="flex items-center justify-between gap-2">
										<span class="text-muted-foreground">建議數量</span>
										<Quantity shares={item.quantity_shares} inline />
									</div>
								{/if}
								{#if item.est_price}
									<div class="flex items-center justify-between gap-2">
										<span class="text-muted-foreground">估計價格</span>
										<span class="tnum">{formatMoney(item.est_price, { dp: 2 })}</span>
									</div>
								{/if}
								{#if item.est_amount}
									<div class="flex items-center justify-between gap-2">
										<span class="text-muted-foreground">估計金額</span>
										<span class="tnum">{formatMoney(item.est_amount)}</span>
									</div>
								{/if}
								{#if item.est_fee_tax}
									<div class="flex items-center justify-between gap-2">
										<span class="text-muted-foreground">估計費稅</span>
										<span class="tnum">{formatMoney(item.est_fee_tax)}</span>
									</div>
								{/if}
								{#if item.confidence}
									<div class="flex items-center justify-between gap-2">
										<span class="text-muted-foreground">信心值</span>
										<span class="tnum">{formatPercent(item.confidence)}</span>
									</div>
								{/if}
								{#if item.current_weight != null || item.target_weight != null}
									<div class="space-y-1 pt-1">
										<span class="text-muted-foreground text-xs">權重變化</span>
										<WeightBar current={item.current_weight} target={item.target_weight} />
									</div>
								{/if}
							</div>
						</Card.Content>
					</Card.Root>
				{/each}
			</div>

			<!-- 頁尾免責聲明 -->
			<Disclaimer
				text={data.disclaimer}
				marketDataAsOf={data.market_data_as_of}
				portfolioAsOf={data.portfolio_as_of}
			/>
		{/if}
	</PageState>
</div>
