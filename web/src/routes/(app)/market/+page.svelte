<script lang="ts">
	import { onMount } from 'svelte';
	import { market as marketApi, ApiError } from '$lib/api';
	import type { FreshnessItem, DailyBar, CorporateAction, CalendarDay } from '$lib/api';
	import { formatMoney, formatDate, formatDateTime } from '$lib/format';
	import { datasetLabel } from '$lib/labels';
	import { chartPalette } from '$lib/nav';

	import PageState from '$lib/components/app/page-state.svelte';
	import StatCard from '$lib/components/app/stat-card.svelte';
	import LineChart, { type Series } from '$lib/components/app/line-chart.svelte';
	import * as Card from '$lib/components/ui/card';
	import * as Table from '$lib/components/ui/table';
	import * as Tabs from '$lib/components/ui/tabs';
	import { Input } from '$lib/components/ui/input';
	import { Label } from '$lib/components/ui/label';
	import { Badge } from '$lib/components/ui/badge';
	import { Button } from '$lib/components/ui/button';

	import DatabaseIcon from '@lucide/svelte/icons/database';
	import CandlestickChartIcon from '@lucide/svelte/icons/chart-candlestick';
	import SparklesIcon from '@lucide/svelte/icons/sparkles';
	import CalendarDaysIcon from '@lucide/svelte/icons/calendar-days';
	import SearchIcon from '@lucide/svelte/icons/search';
	import CalendarClockIcon from '@lucide/svelte/icons/calendar-clock';

	let tab = $state('freshness');

	// --- 工具：日期字串 ---
	function isoDate(d: Date): string {
		const pad = (n: number) => n.toString().padStart(2, '0');
		return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`;
	}
	function daysAgo(n: number): string {
		const d = new Date();
		d.setDate(d.getDate() - n);
		return isoDate(d);
	}
	const today = isoDate(new Date());

	// =====================================================================
	// 分頁一：資料新鮮度
	// =====================================================================
	let freshness = $state<FreshnessItem[]>([]);
	let freshLoading = $state(true);
	let freshError = $state<unknown>(null);

	async function loadFreshness() {
		freshLoading = true;
		freshError = null;
		try {
			freshness = await marketApi.freshness();
		} catch (e) {
			freshError = e;
		} finally {
			freshLoading = false;
		}
	}

	const latestOverall = $derived.by(() => {
		const dates = freshness
			.map((f) => f.latest_data_date)
			.filter((d): d is string => !!d)
			.sort();
		return dates.length ? dates[dates.length - 1] : null;
	});

	function staleTone(days: number | null | undefined): 'up' | 'flat' | 'down' {
		if (days === null || days === undefined) return 'flat';
		if (days >= 5) return 'down';
		if (days >= 2) return 'flat';
		return 'up';
	}
	function staleBadgeClass(days: number | null | undefined): string {
		const t = staleTone(days);
		if (t === 'down') return 'border-down/40 text-down bg-down/10';
		if (t === 'flat')
			return 'border-amber-500/40 text-amber-600 bg-amber-500/10 dark:text-amber-400';
		return 'border-up/40 text-up bg-up/10';
	}
	function staleLabel(days: number | null | undefined): string {
		if (days === null || days === undefined) return '未知';
		if (days <= 0) return '最新';
		return `落後 ${days} 個交易日`;
	}

	function statusBadgeClass(status: string): string {
		const ok = status === 'success' || status === 'ok' || status === 'completed';
		const fail = status === 'failed' || status === 'error';
		if (ok) return 'border-up/40 text-up bg-up/10';
		if (fail) return 'border-down/40 text-down bg-down/10';
		return '';
	}

	// =====================================================================
	// 分頁二：日 K
	// =====================================================================
	let symbol = $state('2330');
	let barsFrom = $state(daysAgo(120));
	let barsTo = $state(today);
	let bars = $state<DailyBar[]>([]);
	let barsLoading = $state(false);
	let barsError = $state<unknown>(null);
	let barsLoaded = $state(false);

	async function loadBars() {
		barsLoading = true;
		barsError = null;
		try {
			const sym = symbol.trim();
			bars = await marketApi.bars({
				symbol: sym || undefined,
				from: barsFrom || undefined,
				to: barsTo || undefined,
				limit: 60
			});
			barsLoaded = true;
		} catch (e) {
			barsError = e;
		} finally {
			barsLoading = false;
		}
	}

	// API 回傳新到舊，圖表與表格需由舊到新
	const barsAsc = $derived([...bars].reverse());

	const closeSeries = $derived.by<Series[]>(() => {
		const pts = barsAsc
			.map((b) => ({ x: b.bar_date, y: Number(b.close) }))
			.filter((p) => Number.isFinite(p.y));
		if (!pts.length) return [];
		return [{ label: '收盤價', color: chartPalette[0], points: pts }];
	});

	const latestBar = $derived(bars.length ? bars[0] : null);

	function onBarsSubmit(e: SubmitEvent) {
		e.preventDefault();
		loadBars();
	}

	// =====================================================================
	// 分頁三：公司行動
	// =====================================================================
	let actions = $state<CorporateAction[]>([]);
	let actionsLoading = $state(false);
	let actionsError = $state<unknown>(null);
	let actionsLoaded = $state(false);

	async function loadActions() {
		actionsLoading = true;
		actionsError = null;
		try {
			actions = await marketApi.corporateActions();
			actionsLoaded = true;
		} catch (e) {
			actionsError = e;
		} finally {
			actionsLoading = false;
		}
	}

	const actionTypeLabels: Record<string, string> = {
		cash_dividend: '現金股利',
		stock_dividend: '股票股利',
		split: '股票分割',
		capital_reduction: '減資'
	};
	function actionTypeLabel(t: string): string {
		return actionTypeLabels[t] ?? t;
	}

	// =====================================================================
	// 分頁四：交易日曆
	// =====================================================================
	let calFrom = $state(daysAgo(30));
	let calTo = $state(daysAgo(-14)); // 往後 14 天
	let calendar = $state<CalendarDay[]>([]);
	let calLoading = $state(false);
	let calError = $state<unknown>(null);
	let calLoaded = $state(false);

	async function loadCalendar() {
		calLoading = true;
		calError = null;
		try {
			calendar = await marketApi.calendar({
				from: calFrom || undefined,
				to: calTo || undefined
			});
			calLoaded = true;
		} catch (e) {
			calError = e;
		} finally {
			calLoading = false;
		}
	}

	function onCalSubmit(e: SubmitEvent) {
		e.preventDefault();
		loadCalendar();
	}

	// =====================================================================
	// 進分頁時延遲載入
	// =====================================================================
	onMount(loadFreshness);

	$effect(() => {
		if (tab === 'bars' && !barsLoaded && !barsLoading) loadBars();
		if (tab === 'actions' && !actionsLoaded && !actionsLoading) loadActions();
		if (tab === 'calendar' && !calLoaded && !calLoading) loadCalendar();
	});
</script>

<div class="space-y-6">
	<div>
		<h1 class="text-2xl font-semibold tracking-tight">市場資料</h1>
		<p class="text-muted-foreground mt-1 text-sm">
			台股日終行情、公司行動、交易日曆，以及各資料集的更新新鮮度。資料由後端 worker 定期匯入。
		</p>
	</div>

	<Tabs.Root bind:value={tab}>
		<Tabs.List>
			<Tabs.Trigger value="freshness"><DatabaseIcon class="size-4" />資料新鮮度</Tabs.Trigger>
			<Tabs.Trigger value="bars"><CandlestickChartIcon class="size-4" />日 K</Tabs.Trigger>
			<Tabs.Trigger value="actions"><SparklesIcon class="size-4" />公司行動</Tabs.Trigger>
			<Tabs.Trigger value="calendar"><CalendarDaysIcon class="size-4" />交易日曆</Tabs.Trigger>
		</Tabs.List>

		<!-- =============================================================== -->
		<!-- 分頁一：資料新鮮度 -->
		<!-- =============================================================== -->
		<Tabs.Content value="freshness" class="space-y-4">
			<PageState
				loading={freshLoading}
				error={freshError}
				empty={freshness.length === 0}
				onRetry={loadFreshness}
				emptyIcon={DatabaseIcon}
				emptyTitle="尚無市場資料"
				emptyHint="後端 worker 尚未匯入任何資料集。待第一次匯入完成後，這裡會顯示各資料集的最新資料日與更新狀態。"
			>
				{#if latestOverall}
					<StatCard label="目前最新資料日" icon={CalendarClockIcon} accent>
						{#snippet value()}{formatDate(latestOverall)}{/snippet}
						{#snippet footer()}所有資料集中最新的一筆資料日期{/snippet}
					</StatCard>
				{/if}

				<Card.Root>
					<Card.Header>
						<Card.Title>各資料集更新狀態</Card.Title>
						<Card.Description>共 {freshness.length} 個資料集</Card.Description>
					</Card.Header>
					<Card.Content class="px-0">
						<div class="scrollbar-thin overflow-x-auto">
							<Table.Root>
								<Table.Header>
									<Table.Row>
										<Table.Head>資料集</Table.Head>
										<Table.Head>最新資料日</Table.Head>
										<Table.Head>新鮮度</Table.Head>
										<Table.Head>匯入狀態</Table.Head>
										<Table.Head>來源</Table.Head>
										<Table.Head class="text-right">最近抓取時間</Table.Head>
									</Table.Row>
								</Table.Header>
								<Table.Body>
									{#each freshness as f (f.dataset)}
										<Table.Row class="hover:bg-muted/40">
											<Table.Cell class="font-medium">{datasetLabel(f.dataset)}</Table.Cell>
											<Table.Cell class="tnum">{formatDate(f.latest_data_date)}</Table.Cell>
											<Table.Cell>
												<Badge
													variant="outline"
													class="font-normal {staleBadgeClass(f.stale_trading_days)}"
												>
													{staleLabel(f.stale_trading_days)}
												</Badge>
											</Table.Cell>
											<Table.Cell>
												<Badge variant="outline" class="font-normal {statusBadgeClass(f.status)}"
													>{f.status}</Badge
												>
											</Table.Cell>
											<Table.Cell class="text-muted-foreground text-sm">{f.source_name}</Table.Cell>
											<Table.Cell class="tnum text-muted-foreground text-right text-sm"
												>{formatDateTime(f.latest_fetched_at)}</Table.Cell
											>
										</Table.Row>
									{/each}
								</Table.Body>
							</Table.Root>
						</div>
					</Card.Content>
				</Card.Root>
			</PageState>
		</Tabs.Content>

		<!-- =============================================================== -->
		<!-- 分頁二：日 K -->
		<!-- =============================================================== -->
		<Tabs.Content value="bars" class="space-y-4">
			<Card.Root>
				<Card.Content class="pt-6">
					<form class="flex flex-wrap items-end gap-3" onsubmit={onBarsSubmit}>
						<div class="grid gap-1.5">
							<Label for="bar-symbol">標的代號</Label>
							<Input id="bar-symbol" bind:value={symbol} placeholder="2330" class="w-32" />
						</div>
						<div class="grid gap-1.5">
							<Label for="bar-from">起始日</Label>
							<Input id="bar-from" type="date" bind:value={barsFrom} class="w-40" />
						</div>
						<div class="grid gap-1.5">
							<Label for="bar-to">結束日</Label>
							<Input id="bar-to" type="date" bind:value={barsTo} class="w-40" />
						</div>
						<Button type="submit" disabled={barsLoading}><SearchIcon class="size-4" />查詢</Button>
					</form>
				</Card.Content>
			</Card.Root>

			<PageState
				loading={barsLoading}
				error={barsError}
				empty={barsLoaded && bars.length === 0}
				onRetry={loadBars}
				emptyIcon={CandlestickChartIcon}
				emptyTitle="查無日 K 資料"
				emptyHint="此標的在所選區間內沒有日終行情。請確認代號正確，或等待後端 worker 匯入該標的的歷史行情。"
			>
				{#if closeSeries.length}
					<Card.Root>
						<Card.Header class="flex flex-row items-center justify-between">
							<div>
								<Card.Title>{symbol} 收盤走勢</Card.Title>
								<Card.Description>近 {bars.length} 個交易日</Card.Description>
							</div>
							{#if latestBar}
								<div class="text-right">
									<div class="text-2xl font-semibold tracking-tight">
										{formatMoney(latestBar.close, { dp: 2 })}
									</div>
									<div class="text-muted-foreground text-xs">
										{formatDate(latestBar.bar_date)} 收盤
									</div>
								</div>
							{/if}
						</Card.Header>
						<Card.Content>
							<LineChart series={closeSeries} />
						</Card.Content>
					</Card.Root>
				{/if}

				<Card.Root>
					<Card.Header>
						<Card.Title>日 K 明細</Card.Title>
						<Card.Description>由新到舊排序</Card.Description>
					</Card.Header>
					<Card.Content class="px-0">
						<div class="scrollbar-thin overflow-x-auto">
							<Table.Root>
								<Table.Header>
									<Table.Row>
										<Table.Head>日期</Table.Head>
										<Table.Head class="text-right">開盤</Table.Head>
										<Table.Head class="text-right">最高</Table.Head>
										<Table.Head class="text-right">最低</Table.Head>
										<Table.Head class="text-right">收盤</Table.Head>
										<Table.Head class="text-right">成交股數</Table.Head>
									</Table.Row>
								</Table.Header>
								<Table.Body>
									{#each bars as b (b.id)}
										<Table.Row class="hover:bg-muted/40">
											<Table.Cell class="tnum font-medium">{formatDate(b.bar_date)}</Table.Cell>
											<Table.Cell class="tnum text-right"
												>{b.open ? formatMoney(b.open, { dp: 2 }) : '—'}</Table.Cell
											>
											<Table.Cell class="tnum text-right"
												>{b.high ? formatMoney(b.high, { dp: 2 }) : '—'}</Table.Cell
											>
											<Table.Cell class="tnum text-right"
												>{b.low ? formatMoney(b.low, { dp: 2 }) : '—'}</Table.Cell
											>
											<Table.Cell class="tnum text-right font-medium"
												>{formatMoney(b.close, { dp: 2 })}</Table.Cell
											>
											<Table.Cell class="tnum text-right"
												>{b.volume_shares ? formatMoney(b.volume_shares) : '—'}</Table.Cell
											>
										</Table.Row>
									{/each}
								</Table.Body>
							</Table.Root>
						</div>
					</Card.Content>
				</Card.Root>
			</PageState>
		</Tabs.Content>

		<!-- =============================================================== -->
		<!-- 分頁三：公司行動 -->
		<!-- =============================================================== -->
		<Tabs.Content value="actions" class="space-y-4">
			<PageState
				loading={actionsLoading}
				error={actionsError}
				empty={actionsLoaded && actions.length === 0}
				onRetry={loadActions}
				emptyIcon={SparklesIcon}
				emptyTitle="尚無公司行動資料"
				emptyHint="目前沒有任何配息、配股、分割或減資紀錄。待後端 worker 匯入公司行動後，這裡會列出各標的的除權息事件。"
			>
				<Card.Root>
					<Card.Header>
						<Card.Title>公司行動</Card.Title>
						<Card.Description>配息、配股、分割與減資紀錄，共 {actions.length} 筆</Card.Description>
					</Card.Header>
					<Card.Content class="px-0">
						<div class="scrollbar-thin overflow-x-auto">
							<Table.Root>
								<Table.Header>
									<Table.Row>
										<Table.Head>標的</Table.Head>
										<Table.Head>類型</Table.Head>
										<Table.Head>除權息日</Table.Head>
										<Table.Head class="text-right">每股現金</Table.Head>
										<Table.Head class="text-right">配股比例</Table.Head>
									</Table.Row>
								</Table.Header>
								<Table.Body>
									{#each actions as a (a.id)}
										<Table.Row class="hover:bg-muted/40">
											<Table.Cell class="font-medium">{a.symbol}</Table.Cell>
											<Table.Cell
												><Badge variant="secondary" class="font-normal"
													>{actionTypeLabel(a.action_type)}</Badge
												></Table.Cell
											>
											<Table.Cell class="tnum">{formatDate(a.ex_date)}</Table.Cell>
											<Table.Cell class="tnum text-right"
												>{a.cash_per_share
													? formatMoney(a.cash_per_share, { dp: 4 })
													: '—'}</Table.Cell
											>
											<Table.Cell class="tnum text-right">{a.stock_ratio ?? '—'}</Table.Cell>
										</Table.Row>
									{/each}
								</Table.Body>
							</Table.Root>
						</div>
					</Card.Content>
				</Card.Root>
			</PageState>
		</Tabs.Content>

		<!-- =============================================================== -->
		<!-- 分頁四：交易日曆 -->
		<!-- =============================================================== -->
		<Tabs.Content value="calendar" class="space-y-4">
			<Card.Root>
				<Card.Content class="pt-6">
					<form class="flex flex-wrap items-end gap-3" onsubmit={onCalSubmit}>
						<div class="grid gap-1.5">
							<Label for="cal-from">起始日</Label>
							<Input id="cal-from" type="date" bind:value={calFrom} class="w-40" />
						</div>
						<div class="grid gap-1.5">
							<Label for="cal-to">結束日</Label>
							<Input id="cal-to" type="date" bind:value={calTo} class="w-40" />
						</div>
						<Button type="submit" disabled={calLoading}><SearchIcon class="size-4" />查詢</Button>
					</form>
				</Card.Content>
			</Card.Root>

			<PageState
				loading={calLoading}
				error={calError}
				empty={calLoaded && calendar.length === 0}
				onRetry={loadCalendar}
				emptyIcon={CalendarDaysIcon}
				emptyTitle="尚無交易日曆資料"
				emptyHint="所選區間內沒有交易日曆紀錄。待後端 worker 匯入台股交易日曆後，這裡會顯示每日的開市與休市狀態。"
			>
				<Card.Root>
					<Card.Header>
						<Card.Title>交易日曆</Card.Title>
						<Card.Description>共 {calendar.length} 天</Card.Description>
					</Card.Header>
					<Card.Content class="px-0">
						<div class="scrollbar-thin overflow-x-auto">
							<Table.Root>
								<Table.Header>
									<Table.Row>
										<Table.Head>日期</Table.Head>
										<Table.Head>狀態</Table.Head>
										<Table.Head>備註</Table.Head>
									</Table.Row>
								</Table.Header>
								<Table.Body>
									{#each calendar as d (d.date)}
										<Table.Row class="hover:bg-muted/40">
											<Table.Cell class="tnum font-medium">{formatDate(d.date)}</Table.Cell>
											<Table.Cell>
												{#if d.is_open}
													<Badge variant="outline" class="border-up/40 text-up bg-up/10 font-normal"
														>開市</Badge
													>
												{:else}
													<Badge variant="outline" class="text-muted-foreground font-normal"
														>休市</Badge
													>
												{/if}
											</Table.Cell>
											<Table.Cell class="text-muted-foreground text-sm">{d.note || '—'}</Table.Cell>
										</Table.Row>
									{/each}
								</Table.Body>
							</Table.Root>
						</div>
					</Card.Content>
				</Card.Root>
			</PageState>
		</Tabs.Content>
	</Tabs.Root>
</div>
