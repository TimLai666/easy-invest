<script lang="ts">
	import { onMount } from 'svelte';
	import { backtests as backtestsApi, ApiError } from '$lib/api';
	import type { BacktestRun, BacktestResultDetail, BacktestRunBody } from '$lib/api';
	import {
		formatMoney,
		formatPercent,
		toneOf,
		toneClass,
		formatDate,
		formatDateTime
	} from '$lib/format';
	import { backtestStatusLabels } from '$lib/labels';
	import { chartPalette } from '$lib/nav';

	import PageState from '$lib/components/app/page-state.svelte';
	import StatCard from '$lib/components/app/stat-card.svelte';
	import Disclaimer from '$lib/components/app/disclaimer.svelte';
	import Money from '$lib/components/ui/money/money.svelte';
	import Quantity from '$lib/components/app/quantity.svelte';
	import LineChart, { type Series } from '$lib/components/app/line-chart.svelte';
	import * as Card from '$lib/components/ui/card';
	import * as Table from '$lib/components/ui/table';
	import * as Alert from '$lib/components/ui/alert';
	import { Button } from '$lib/components/ui/button';
	import { Input } from '$lib/components/ui/input';
	import { Label } from '$lib/components/ui/label';
	import { Badge } from '$lib/components/ui/badge';
	import { Textarea } from '$lib/components/ui/textarea';
	import { Skeleton } from '$lib/components/ui/skeleton';
	import { toast } from '$lib/components/ui/sonner';

	import LineChartIcon from '@lucide/svelte/icons/chart-line';
	import PlayIcon from '@lucide/svelte/icons/play';
	import TrendingUpIcon from '@lucide/svelte/icons/trending-up';
	import TrendingDownIcon from '@lucide/svelte/icons/trending-down';
	import GaugeIcon from '@lucide/svelte/icons/gauge';
	import AlertCircleIcon from '@lucide/svelte/icons/circle-alert';
	import Loader2Icon from '@lucide/svelte/icons/loader-circle';

	// 清單狀態
	let runs = $state<BacktestRun[]>([]);
	let listLoading = $state(true);
	let listError = $state<unknown>(null);

	// 選取的 run
	let selected = $state<BacktestRun | null>(null);
	let selectedLoading = $state(false);

	// 表單欄位
	let fName = $state('');
	let fFrom = $state('');
	let fTo = $state('');
	let fInitialCash = $state('');
	let fBenchmark = $state('0050');
	let fMonthlyAmount = $state('');
	let fSymbols = $state('');
	let fTargetWeights = $state('');
	let submitting = $state(false);

	async function loadList() {
		listLoading = true;
		listError = null;
		try {
			runs = await backtestsApi.list();
		} catch (e) {
			listError = e;
		} finally {
			listLoading = false;
		}
	}
	onMount(loadList);

	async function selectRun(id: string) {
		selectedLoading = true;
		try {
			selected = await backtestsApi.get(id);
		} catch (e) {
			toast.error(e instanceof ApiError ? e.message : '載入回測結果失敗');
		} finally {
			selectedLoading = false;
		}
	}

	function parseSymbols(raw: string): string[] {
		return raw
			.split(/[,\n]/)
			.map((s) => s.trim())
			.filter((s) => s.length > 0);
	}

	function parseTargetWeights(raw: string): Record<string, string> | null {
		const lines = raw
			.split(/\n/)
			.map((l) => l.trim())
			.filter((l) => l.length > 0);
		if (lines.length === 0) return null;
		const out: Record<string, string> = {};
		for (const line of lines) {
			const [sym, w] = line.split(':').map((p) => p.trim());
			if (!sym || !w) throw new Error(`目標權重格式錯誤：「${line}」，應為 symbol:weight`);
			out[sym] = w;
		}
		return out;
	}

	async function submit(e: SubmitEvent) {
		e.preventDefault();
		if (!fInitialCash.trim()) {
			toast.error('請填寫期初資金');
			return;
		}
		let targetWeights: Record<string, string> | null;
		try {
			targetWeights = parseTargetWeights(fTargetWeights);
		} catch (err) {
			toast.error(err instanceof Error ? err.message : '目標權重格式錯誤');
			return;
		}
		const symbols = parseSymbols(fSymbols);

		const body: BacktestRunBody = {
			initial_cash: fInitialCash.trim()
		};
		if (fName.trim()) body.name = fName.trim();
		if (fFrom) body.from = fFrom;
		if (fTo) body.to = fTo;
		if (fBenchmark.trim()) body.benchmark_symbol = fBenchmark.trim();
		if (fMonthlyAmount.trim()) body.monthly_amount = fMonthlyAmount.trim();
		if (symbols.length) body.symbols = symbols;
		if (targetWeights) body.target_weights = targetWeights;

		submitting = true;
		try {
			const run = await backtestsApi.run(body);
			toast.success('回測已建立');
			selected = run;
			await loadList();
			// 若回傳尚在計算中，嘗試重新取得最新狀態
			if (run.status === 'pending') {
				await selectRun(run.id);
			}
		} catch (err) {
			toast.error(err instanceof ApiError ? err.message : '建立回測失敗');
		} finally {
			submitting = false;
		}
	}

	// 結果衍生
	const result = $derived(selected?.result ?? null);

	const series = $derived.by<Series[]>(() => {
		if (!result) return [];
		const out: Series[] = [];
		const mapCurve = (d: BacktestResultDetail | undefined) =>
			(d?.equity_curve ?? []).map((p) => ({ x: p.date, y: Number(p.equity) }));

		out.push({ label: '策略', color: chartPalette[0], points: mapCurve(result.strategy) });
		if (result.benchmark_buy_hold) {
			out.push({
				label: '買進持有基準',
				color: chartPalette[1],
				points: mapCurve(result.benchmark_buy_hold)
			});
		}
		if (result.benchmark_dca) {
			out.push({
				label: '定期定額基準',
				color: chartPalette[2],
				points: mapCurve(result.benchmark_dca)
			});
		}
		return out.filter((s) => s.points.length > 0);
	});

	const trades = $derived(result?.strategy.trades ?? []);
</script>

<div class="space-y-6">
	<div>
		<h1 class="text-xl font-semibold tracking-tight">策略回測</h1>
		<p class="text-muted-foreground mt-1 text-sm">
			以歷史日終資料模擬策略表現，並與買進持有、定期定額基準對照。回測結果為歷史模擬，不代表未來績效。
		</p>
	</div>

	<div class="grid gap-6 lg:grid-cols-3">
		<!-- 建立回測 -->
		<Card.Root class="lg:col-span-1 self-start">
			<Card.Header>
				<Card.Title>建立回測</Card.Title>
				<Card.Description>設定期間與資金，產生一次回測</Card.Description>
			</Card.Header>
			<Card.Content>
				<form class="space-y-4" onsubmit={submit}>
					<div class="space-y-1.5">
						<Label for="bt-name">名稱</Label>
						<Input id="bt-name" bind:value={fName} placeholder="例：2330+0050 再平衡" />
					</div>

					<div class="grid grid-cols-2 gap-3">
						<div class="space-y-1.5">
							<Label for="bt-from">起日</Label>
							<Input id="bt-from" type="date" bind:value={fFrom} />
						</div>
						<div class="space-y-1.5">
							<Label for="bt-to">迄日</Label>
							<Input id="bt-to" type="date" bind:value={fTo} />
						</div>
					</div>

					<div class="space-y-1.5">
						<Label for="bt-cash">期初資金（必填）</Label>
						<Input
							id="bt-cash"
							inputmode="decimal"
							bind:value={fInitialCash}
							placeholder="例：1000000"
							required
						/>
						<p class="text-muted-foreground text-xs">單位 TWD。</p>
					</div>

					<div class="space-y-1.5">
						<Label for="bt-bench">基準標的</Label>
						<Input id="bt-bench" bind:value={fBenchmark} placeholder="0050" />
					</div>

					<div class="space-y-1.5">
						<Label for="bt-monthly">每月定期定額金額</Label>
						<Input
							id="bt-monthly"
							inputmode="decimal"
							bind:value={fMonthlyAmount}
							placeholder="選填，例：10000"
						/>
						<p class="text-muted-foreground text-xs">用於定期定額基準對照，留空則不計。</p>
					</div>

					<div class="space-y-1.5">
						<Label for="bt-symbols">標的清單</Label>
						<Textarea
							id="bt-symbols"
							bind:value={fSymbols}
							placeholder="逗號或換行分隔，例：2330,0050,006208"
							rows={2}
						/>
						<p class="text-muted-foreground text-xs">選填，留空則使用目標權重或設定中的標的。</p>
					</div>

					<div class="space-y-1.5">
						<Label for="bt-weights">目標權重</Label>
						<Textarea
							id="bt-weights"
							bind:value={fTargetWeights}
							placeholder={'每行一筆 symbol:weight\n例：\n2330:0.4\n0050:0.6'}
							rows={3}
						/>
						<p class="text-muted-foreground text-xs">選填，留空則沿用設定中的目標權重。</p>
					</div>

					<Button type="submit" class="w-full" disabled={submitting}>
						{#if submitting}<Loader2Icon class="size-4 animate-spin" />計算中{:else}<PlayIcon
								class="size-4"
							/>執行回測{/if}
					</Button>
				</form>
			</Card.Content>
		</Card.Root>

		<!-- 回測結果 -->
		<div class="space-y-6 lg:col-span-2">
			<Card.Root>
				<Card.Header>
					<div class="flex flex-wrap items-center justify-between gap-2">
						<div>
							<Card.Title>回測結果</Card.Title>
							<Card.Description>
								{#if selected}{selected.name || '未命名回測'}　·　{formatDateTime(
										selected.created_at
									)}{:else}選取或建立一個回測以檢視結果{/if}
							</Card.Description>
						</div>
						{#if selected}
							<Badge
								variant={selected.status === 'completed'
									? 'secondary'
									: selected.status === 'failed'
										? 'destructive'
										: 'outline'}
							>
								{backtestStatusLabels[selected.status] ?? selected.status}
							</Badge>
						{/if}
					</div>
				</Card.Header>
				<Card.Content>
					{#if selectedLoading}
						<div class="space-y-4">
							<Skeleton class="h-[260px] w-full" />
							<div class="grid gap-4 sm:grid-cols-3">
								<Skeleton class="h-24" /><Skeleton class="h-24" /><Skeleton class="h-24" />
							</div>
						</div>
					{:else if !selected}
						<div class="flex flex-col items-center gap-3 py-12 text-center">
							<div class="bg-muted/50 rounded-full p-3">
								<LineChartIcon class="text-muted-foreground size-6" />
							</div>
							<div>
								<p class="font-medium">尚未選取回測</p>
								<p class="text-muted-foreground mt-1 text-sm">
									在左側填寫期初資金並執行回測，或從下方歷史清單點選一筆。
								</p>
							</div>
						</div>
					{:else if selected.status === 'failed'}
						<Alert.Root variant="destructive">
							<AlertCircleIcon class="size-4" />
							<Alert.Title>回測失敗</Alert.Title>
							<Alert.Description
								>{selected.error_message ||
									'計算過程發生未知錯誤，請調整參數後重試。'}</Alert.Description
							>
						</Alert.Root>
					{:else if selected.status === 'pending'}
						<div class="flex flex-col items-center gap-3 py-12 text-center">
							<Loader2Icon class="text-muted-foreground size-6 animate-spin" />
							<div>
								<p class="font-medium">計算中</p>
								<p class="text-muted-foreground mt-1 text-sm">
									回測正在背景計算，稍後可從歷史清單重新點選查看。
								</p>
							</div>
						</div>
					{:else if result}
						<div class="space-y-6">
							<!-- 權益曲線 -->
							{#if series.length}
								<LineChart {series} yPrefix="$" />
							{:else}
								<p class="text-muted-foreground py-8 text-center text-sm">此回測沒有權益曲線資料</p>
							{/if}

							<!-- 績效指標 -->
							<div class="grid gap-4 sm:grid-cols-3">
								<StatCard label="總報酬" icon={TrendingUpIcon}>
									{#snippet value()}
										<span class={toneClass(toneOf(result.strategy.total_return))}>
											{formatPercent(result.strategy.total_return, 2, { sign: true })}
										</span>
									{/snippet}
									{#snippet footer()}期末權益 {formatMoney(result.strategy.final_equity)}{/snippet}
								</StatCard>
								<StatCard label="年化報酬" icon={GaugeIcon}>
									{#snippet value()}
										<span class={toneClass(toneOf(result.strategy.annualized_return))}>
											{formatPercent(result.strategy.annualized_return, 2, { sign: true })}
										</span>
									{/snippet}
									{#snippet footer()}期初權益 {formatMoney(
											result.strategy.initial_equity
										)}{/snippet}
								</StatCard>
								<StatCard label="最大回撤" icon={TrendingDownIcon}>
									{#snippet value()}
										<span class="text-down">{formatPercent(result.strategy.max_drawdown, 2)}</span>
									{/snippet}
									{#snippet footer()}期末現金 {formatMoney(result.strategy.final_cash)}{/snippet}
								</StatCard>
							</div>

							<!-- 假設說明 -->
							{#if result.assumptions || result.benchmark_note}
								<div
									class="text-muted-foreground bg-muted/40 space-y-1 rounded-lg border border-dashed p-3 text-xs leading-relaxed"
								>
									{#if result.assumptions}<p>{result.assumptions}</p>{/if}
									{#if result.benchmark_note}<p>{result.benchmark_note}</p>{/if}
								</div>
							{/if}

							<!-- 交易清單 -->
							<div>
								<h3 class="mb-2 text-sm font-medium">策略交易明細（共 {trades.length} 筆）</h3>
								{#if trades.length}
									<div class="scrollbar-thin overflow-x-auto rounded-lg border">
										<Table.Root>
											<Table.Header>
												<Table.Row>
													<Table.Head>日期</Table.Head>
													<Table.Head>標的</Table.Head>
													<Table.Head>動作</Table.Head>
													<Table.Head class="text-right">數量</Table.Head>
													<Table.Head class="text-right">價格</Table.Head>
													<Table.Head class="text-right">費稅</Table.Head>
													<Table.Head class="text-right">現金變動</Table.Head>
												</Table.Row>
											</Table.Header>
											<Table.Body>
												{#each trades as t, i (i)}
													<Table.Row class="hover:bg-muted/40">
														<Table.Cell class="whitespace-nowrap">{formatDate(t.date)}</Table.Cell>
														<Table.Cell class="font-medium">{t.symbol}</Table.Cell>
														<Table.Cell>
															<Badge
																variant={t.action === 'buy' ? 'secondary' : 'outline'}
																class="font-normal"
															>
																{t.action === 'buy'
																	? '買進'
																	: t.action === 'sell'
																		? '賣出'
																		: t.action}
															</Badge>
														</Table.Cell>
														<Table.Cell class="text-right"
															><Quantity shares={t.quantity_shares} inline /></Table.Cell
														>
														<Table.Cell class="tnum text-right"
															>{formatMoney(t.price, { dp: 2 })}</Table.Cell
														>
														<Table.Cell class="tnum text-right"
															>{formatMoney(t.fee, { dp: 0 })} / {formatMoney(t.tax, {
																dp: 0
															})}</Table.Cell
														>
														<Table.Cell class="text-right"
															><Money value={t.cash_delta} sign /></Table.Cell
														>
													</Table.Row>
												{/each}
											</Table.Body>
										</Table.Root>
									</div>
								{:else}
									<p class="text-muted-foreground text-sm">此回測期間沒有產生任何交易。</p>
								{/if}
							</div>

							<Disclaimer
								text="回測結果為依歷史日終市場資料進行之模擬，已盡量納入費稅假設，但仍與實際成交存在落差，過去績效不代表未來表現。"
							/>
						</div>
					{/if}
				</Card.Content>
			</Card.Root>
		</div>
	</div>

	<!-- 歷史回測 -->
	<Card.Root>
		<Card.Header>
			<Card.Title>歷史回測</Card.Title>
			<Card.Description>點擊任一筆載入完整結果</Card.Description>
		</Card.Header>
		<Card.Content class="px-0">
			<PageState
				loading={listLoading}
				error={listError}
				empty={runs.length === 0}
				onRetry={loadList}
				emptyIcon={LineChartIcon}
				emptyTitle="還沒有任何回測"
				emptyHint="在上方填寫期初資金與期間，建立你的第一個策略回測。"
			>
				{#snippet skeleton()}
					<div class="space-y-2 px-6">
						<Skeleton class="h-12 w-full" /><Skeleton class="h-12 w-full" /><Skeleton
							class="h-12 w-full"
						/>
					</div>
				{/snippet}
				<div class="scrollbar-thin overflow-x-auto">
					<Table.Root>
						<Table.Header>
							<Table.Row>
								<Table.Head>名稱</Table.Head>
								<Table.Head>狀態</Table.Head>
								<Table.Head class="text-right">總報酬</Table.Head>
								<Table.Head class="text-right">年化</Table.Head>
								<Table.Head class="text-right">最大回撤</Table.Head>
								<Table.Head>建立時間</Table.Head>
								<Table.Head class="text-right"></Table.Head>
							</Table.Row>
						</Table.Header>
						<Table.Body>
							{#each runs as r (r.id)}
								<Table.Row
									class="hover:bg-muted/40 cursor-pointer {selected?.id === r.id
										? 'bg-muted/40'
										: ''}"
									onclick={() => selectRun(r.id)}
								>
									<Table.Cell class="font-medium">{r.name || '未命名回測'}</Table.Cell>
									<Table.Cell>
										<Badge
											variant={r.status === 'completed'
												? 'secondary'
												: r.status === 'failed'
													? 'destructive'
													: 'outline'}
											class="font-normal"
										>
											{backtestStatusLabels[r.status] ?? r.status}
										</Badge>
									</Table.Cell>
									<Table.Cell class="text-right">
										{#if r.result?.strategy}
											<span class={toneClass(toneOf(r.result.strategy.total_return))}>
												{formatPercent(r.result.strategy.total_return, 2, { sign: true })}
											</span>
										{:else}—{/if}
									</Table.Cell>
									<Table.Cell class="text-right">
										{#if r.result?.strategy}
											<span class={toneClass(toneOf(r.result.strategy.annualized_return))}>
												{formatPercent(r.result.strategy.annualized_return, 2, { sign: true })}
											</span>
										{:else}—{/if}
									</Table.Cell>
									<Table.Cell class="text-down text-right">
										{#if r.result?.strategy}{formatPercent(
												r.result.strategy.max_drawdown,
												2
											)}{:else}—{/if}
									</Table.Cell>
									<Table.Cell class="whitespace-nowrap">{formatDateTime(r.created_at)}</Table.Cell>
									<Table.Cell class="text-right">
										<Button
											variant="ghost"
											size="sm"
											onclick={(e) => {
												e.stopPropagation();
												selectRun(r.id);
											}}>檢視</Button
										>
									</Table.Cell>
								</Table.Row>
							{/each}
						</Table.Body>
					</Table.Root>
				</div>
			</PageState>
		</Card.Content>
	</Card.Root>
</div>
