<script lang="ts">
	import { onMount } from 'svelte';
	import { ledger, ApiError } from '$lib/api';
	import type { CreateEventRequest, EventType, LedgerEvent, Unit } from '$lib/api';
	import { unitToShares, formatMoney, formatDate, formatDateTime } from '$lib/format';
	import { eventTypeLabels, userEventTypes } from '$lib/labels';
	import { toast } from '$lib/components/ui/sonner';

	import PageState from '$lib/components/app/page-state.svelte';
	import Quantity from '$lib/components/app/quantity.svelte';
	import Pnl from '$lib/components/app/pnl.svelte';
	import * as Card from '$lib/components/ui/card';
	import * as Table from '$lib/components/ui/table';
	import * as Select from '$lib/components/ui/select';
	import * as Dialog from '$lib/components/ui/dialog';
	import { Button } from '$lib/components/ui/button';
	import { Input } from '$lib/components/ui/input';
	import { Label } from '$lib/components/ui/label';
	import { Textarea } from '$lib/components/ui/textarea';
	import { Badge } from '$lib/components/ui/badge';

	import ReceiptTextIcon from '@lucide/svelte/icons/receipt-text';
	import PlusIcon from '@lucide/svelte/icons/plus';
	import Ban from '@lucide/svelte/icons/ban';

	// ---- 清單狀態 ----
	let events = $state<LedgerEvent[]>([]);
	let loading = $state(true);
	let error = $state<unknown>(null);

	// ---- 篩選 ----
	let filterSymbol = $state('');
	let filterType = $state<string>('all');
	let filterFrom = $state('');
	let filterTo = $state('');

	async function load() {
		loading = true;
		error = null;
		try {
			events = await ledger.list({
				limit: 200,
				asset: filterSymbol.trim() || undefined,
				type: filterType === 'all' ? undefined : filterType,
				from: filterFrom || undefined,
				to: filterTo || undefined
			});
		} catch (e) {
			error = e;
		} finally {
			loading = false;
		}
	}
	onMount(load);

	// 篩選變更即重查（去抖）
	let filterTimer: ReturnType<typeof setTimeout> | null = null;
	function onFilterChange() {
		if (filterTimer) clearTimeout(filterTimer);
		filterTimer = setTimeout(load, 300);
	}

	// ---- 表單狀態 ----
	const today = new Date().toISOString().slice(0, 10);
	let formType = $state<EventType>('buy');
	let symbol = $state('');
	let tradeDate = $state(today);
	let quantity = $state('');
	let unit = $state<Unit | ''>('');
	let price = $state('');
	let fee = $state('');
	let tax = $state('');
	let grossAmount = $state('');
	let splitRatio = $state('');
	let cashDelta = $state('');
	let notes = $state('');

	let submitting = $state(false);
	let fieldErrors = $state<Record<string, string>>({});

	// 依事件類型決定要顯示的欄位
	const showSymbol = $derived(
		[
			'buy',
			'sell',
			'cash_dividend',
			'stock_dividend',
			'split',
			'capital_reduction',
			'manual_correction'
		].includes(formType)
	);
	const symbolRequired = $derived(
		['buy', 'sell', 'cash_dividend', 'stock_dividend', 'split', 'capital_reduction'].includes(
			formType
		)
	);
	const showQuantity = $derived(
		['buy', 'sell', 'stock_dividend', 'manual_correction'].includes(formType)
	);
	const showPrice = $derived(['buy', 'sell'].includes(formType));
	const showFee = $derived(['buy', 'sell'].includes(formType));
	const showTax = $derived(formType === 'sell');
	const showGross = $derived(
		['cash_dividend', 'capital_reduction', 'cash_deposit', 'cash_withdraw'].includes(formType)
	);
	const grossRequired = $derived(
		['cash_dividend', 'cash_deposit', 'cash_withdraw'].includes(formType)
	);
	const showSplitRatio = $derived(formType === 'split');
	const showCashDelta = $derived(formType === 'manual_correction');
	const showNotes = $derived(formType === 'manual_correction');

	// 數量換算預覽
	const previewShares = $derived.by(() => {
		if (!showQuantity || !quantity || !unit) return null;
		return unitToShares(quantity, unit);
	});

	function resetForm() {
		symbol = '';
		tradeDate = today;
		quantity = '';
		unit = '';
		price = '';
		fee = '';
		tax = '';
		grossAmount = '';
		splitRatio = '';
		cashDelta = '';
		notes = '';
		fieldErrors = {};
	}

	function onTypeChange() {
		// 切換類型時清掉與新類型無關的欄位錯誤
		fieldErrors = {};
	}

	async function submit(e: SubmitEvent) {
		e.preventDefault();
		fieldErrors = {};

		// 前端基本檢查（單位必填、不可預設猜）
		if (showQuantity && quantity && !unit) {
			fieldErrors = { unit: '請選擇數量單位（股或張）' };
			toast.error('請選擇數量單位');
			return;
		}

		const body: CreateEventRequest = {
			event_type: formType,
			trade_date: tradeDate
		};

		if (showSymbol && symbol.trim()) body.symbol = symbol.trim();

		if (showQuantity && quantity) {
			body.quantity = quantity;
			if (unit) body.unit = unit;
		}
		if (showPrice && price) body.price = price;

		// fee / tax 留空 = 系統估算，不放進 body
		if (showFee && fee.trim()) body.fee = fee;
		if (showTax && tax.trim()) body.tax = tax;

		if (showGross && grossAmount.trim()) body.gross_amount = grossAmount;

		if (showSplitRatio && splitRatio.trim()) {
			body.metadata = { split_ratio: splitRatio.trim() };
		}
		if (showCashDelta && cashDelta.trim()) {
			body.metadata = { ...(body.metadata ?? {}), cash_delta: cashDelta.trim() };
		}
		if (showNotes && notes.trim()) body.notes = notes.trim();

		submitting = true;
		try {
			await ledger.create(body);
			toast.success(`已新增「${eventTypeLabels[formType]}」事件`);
			resetForm();
			await load();
		} catch (err) {
			if (err instanceof ApiError && err.code === 'validation_failed' && err.details?.length) {
				const map: Record<string, string> = {};
				for (const d of err.details) {
					if (d.field) map[d.field] = d.issue;
				}
				// 後端可能用 metadata.split_ratio 之類路徑，對應到表單欄位
				fieldErrors = map;
				toast.error('表單驗證未通過，請檢查標示的欄位');
			} else {
				toast.error(err instanceof ApiError ? err.message : '新增失敗，請稍後再試');
			}
		} finally {
			submitting = false;
		}
	}

	function fe(field: string): string | undefined {
		return fieldErrors[field];
	}

	// ---- 作廢 ----
	let voidOpen = $state(false);
	let voidTarget = $state<LedgerEvent | null>(null);
	let voidReason = $state('');
	let voiding = $state(false);

	function openVoid(ev: LedgerEvent) {
		voidTarget = ev;
		voidReason = '';
		voidOpen = true;
	}

	async function confirmVoid() {
		if (!voidTarget) return;
		if (!voidReason.trim()) {
			toast.error('請填寫作廢原因');
			return;
		}
		voiding = true;
		try {
			await ledger.void(voidTarget.id, voidReason.trim());
			toast.success('事件已作廢');
			voidOpen = false;
			voidTarget = null;
			await load();
		} catch (err) {
			toast.error(err instanceof ApiError ? err.message : '作廢失敗，請稍後再試');
		} finally {
			voiding = false;
		}
	}
</script>

<div class="space-y-6">
	<div>
		<h1 class="text-2xl font-semibold tracking-tight">交易流水</h1>
		<p class="text-muted-foreground mt-1 text-sm">
			庫存由這裡的事件計算而來。歷史紀錄不可編輯或刪除，只能作廢；需要修正請改用調整事件。
		</p>
	</div>

	<div class="grid gap-6 lg:grid-cols-5">
		<!-- 左：新增事件表單 -->
		<Card.Root class="lg:col-span-2 h-fit">
			<Card.Header>
				<Card.Title>新增事件</Card.Title>
				<Card.Description>選擇事件類型後填寫對應欄位</Card.Description>
			</Card.Header>
			<Card.Content>
				<form class="space-y-4" onsubmit={submit}>
					<!-- 事件類型 -->
					<div class="space-y-1.5">
						<Label for="event-type">事件類型</Label>
						<Select.Root type="single" bind:value={formType} onValueChange={onTypeChange}>
							<Select.Trigger id="event-type" class="w-full">
								{formType ? eventTypeLabels[formType] : '請選擇'}
							</Select.Trigger>
							<Select.Content>
								{#each userEventTypes as t (t)}
									<Select.Item value={t} label={eventTypeLabels[t]}
										>{eventTypeLabels[t]}</Select.Item
									>
								{/each}
							</Select.Content>
						</Select.Root>
					</div>

					<!-- 標的 -->
					{#if showSymbol}
						<div class="space-y-1.5">
							<Label for="symbol"
								>標的代號{#if !symbolRequired}<span class="text-muted-foreground">
										(選填)</span
									>{/if}</Label
							>
							<Input
								id="symbol"
								placeholder="例如 2330"
								bind:value={symbol}
								aria-invalid={!!fe('symbol')}
							/>
							{#if fe('symbol')}<p class="text-destructive text-xs">{fe('symbol')}</p>{/if}
						</div>
					{/if}

					<!-- 交易日 -->
					<div class="space-y-1.5">
						<Label for="trade-date">交易日</Label>
						<Input
							id="trade-date"
							type="date"
							bind:value={tradeDate}
							aria-invalid={!!fe('trade_date')}
						/>
						{#if fe('trade_date')}<p class="text-destructive text-xs">{fe('trade_date')}</p>{/if}
					</div>

					<!-- 數量 + 單位 -->
					{#if showQuantity}
						<div class="grid grid-cols-2 gap-3">
							<div class="space-y-1.5">
								<Label for="quantity"
									>數量{#if formType === 'manual_correction'}<span class="text-muted-foreground">
											(選填)</span
										>{/if}</Label
								>
								<Input
									id="quantity"
									type="number"
									step="any"
									min="0"
									placeholder="0"
									bind:value={quantity}
									aria-invalid={!!fe('quantity')}
								/>
							</div>
							<div class="space-y-1.5">
								<Label for="unit">單位</Label>
								<Select.Root type="single" bind:value={unit}>
									<Select.Trigger id="unit" class="w-full" aria-invalid={!!fe('unit')}>
										{unit === 'lot' ? '張' : unit === 'share' ? '股' : '請選擇'}
									</Select.Trigger>
									<Select.Content>
										<Select.Item value="lot" label="張">張（1 張 = 1000 股）</Select.Item>
										<Select.Item value="share" label="股">股</Select.Item>
									</Select.Content>
								</Select.Root>
							</div>
						</div>
						{#if fe('quantity')}<p class="text-destructive text-xs">{fe('quantity')}</p>{/if}
						{#if fe('unit')}<p class="text-destructive text-xs">{fe('unit')}</p>{/if}
						{#if previewShares}
							<p class="text-muted-foreground text-xs">
								= <Quantity shares={previewShares} inline />
							</p>
						{/if}
					{/if}

					<!-- 價格 -->
					{#if showPrice}
						<div class="space-y-1.5">
							<Label for="price">價格（每股）</Label>
							<Input
								id="price"
								type="number"
								step="any"
								min="0"
								placeholder="0.00"
								bind:value={price}
								aria-invalid={!!fe('price')}
							/>
							{#if fe('price')}<p class="text-destructive text-xs">{fe('price')}</p>{/if}
						</div>
					{/if}

					<!-- 手續費 -->
					{#if showFee}
						<div class="space-y-1.5">
							<Label for="fee">手續費</Label>
							<Input
								id="fee"
								type="number"
								step="any"
								min="0"
								placeholder="留空由系統依設定估算"
								bind:value={fee}
								aria-invalid={!!fe('fee')}
							/>
							<p class="text-muted-foreground text-xs">留空＝依手續費率與折扣自動估算。</p>
							{#if fe('fee')}<p class="text-destructive text-xs">{fe('fee')}</p>{/if}
						</div>
					{/if}

					<!-- 稅 -->
					{#if showTax}
						<div class="space-y-1.5">
							<Label for="tax">證券交易稅</Label>
							<Input
								id="tax"
								type="number"
								step="any"
								min="0"
								placeholder="留空由系統估算"
								bind:value={tax}
								aria-invalid={!!fe('tax')}
							/>
							<p class="text-muted-foreground text-xs">留空＝依台股交易稅率自動估算。</p>
							{#if fe('tax')}<p class="text-destructive text-xs">{fe('tax')}</p>{/if}
						</div>
					{/if}

					<!-- 金額（現金股利／減資／存提）-->
					{#if showGross}
						<div class="space-y-1.5">
							<Label for="gross">
								{#if formType === 'cash_dividend'}股利總金額{:else if formType === 'capital_reduction'}退還現金{:else if formType === 'cash_deposit'}存入金額{:else}提出金額{/if}
								{#if !grossRequired}<span class="text-muted-foreground"> (選填)</span>{/if}
							</Label>
							<Input
								id="gross"
								type="number"
								step="any"
								min="0"
								placeholder="0"
								bind:value={grossAmount}
								aria-invalid={!!fe('gross_amount')}
							/>
							{#if formType === 'cash_dividend'}<p class="text-muted-foreground text-xs">
									可直接填發放的現金股利總額。
								</p>{/if}
							{#if fe('gross_amount')}<p class="text-destructive text-xs">
									{fe('gross_amount')}
								</p>{/if}
						</div>
					{/if}

					<!-- 分割比例 -->
					{#if showSplitRatio}
						<div class="space-y-1.5">
							<Label for="split-ratio">分割比例</Label>
							<Input
								id="split-ratio"
								type="number"
								step="any"
								min="0"
								placeholder="例如 2"
								bind:value={splitRatio}
								aria-invalid={!!fe('metadata.split_ratio') || !!fe('split_ratio')}
							/>
							<p class="text-muted-foreground text-xs">新股數 / 舊股數，例如 2 代表 1 拆 2。</p>
							{#if fe('metadata.split_ratio') || fe('split_ratio')}<p
									class="text-destructive text-xs"
								>
									{fe('metadata.split_ratio') ?? fe('split_ratio')}
								</p>{/if}
						</div>
					{/if}

					<!-- 現金調整（手動更正）-->
					{#if showCashDelta}
						<div class="space-y-1.5">
							<Label for="cash-delta"
								>現金調整<span class="text-muted-foreground"> (選填)</span></Label
							>
							<Input
								id="cash-delta"
								type="number"
								step="any"
								placeholder="正數為增加、負數為減少"
								bind:value={cashDelta}
								aria-invalid={!!fe('metadata.cash_delta') || !!fe('cash_delta')}
							/>
							{#if fe('metadata.cash_delta') || fe('cash_delta')}<p
									class="text-destructive text-xs"
								>
									{fe('metadata.cash_delta') ?? fe('cash_delta')}
								</p>{/if}
						</div>
					{/if}

					<!-- 備註 -->
					{#if showNotes}
						<div class="space-y-1.5">
							<Label for="notes">備註</Label>
							<Textarea
								id="notes"
								rows={2}
								placeholder="說明此筆更正的原因與依據"
								bind:value={notes}
							/>
							{#if fe('notes')}<p class="text-destructive text-xs">{fe('notes')}</p>{/if}
						</div>
					{/if}

					<Button type="submit" class="w-full" disabled={submitting}>
						<PlusIcon class="size-4" />{submitting ? '送出中…' : '新增事件'}
					</Button>
				</form>
			</Card.Content>
		</Card.Root>

		<!-- 右：事件流清單 -->
		<Card.Root class="lg:col-span-3">
			<Card.Header>
				<Card.Title>事件流</Card.Title>
				<Card.Description>依交易日排序的所有事件</Card.Description>
				<!-- 篩選列 -->
				<div class="grid grid-cols-2 gap-2 pt-3 sm:grid-cols-4">
					<Input placeholder="標的代號" bind:value={filterSymbol} oninput={onFilterChange} />
					<Select.Root type="single" bind:value={filterType} onValueChange={load}>
						<Select.Trigger class="w-full">
							{filterType === 'all' ? '全部類型' : eventTypeLabels[filterType as EventType]}
						</Select.Trigger>
						<Select.Content>
							<Select.Item value="all" label="全部類型">全部類型</Select.Item>
							{#each userEventTypes as t (t)}
								<Select.Item value={t} label={eventTypeLabels[t]}>{eventTypeLabels[t]}</Select.Item>
							{/each}
						</Select.Content>
					</Select.Root>
					<Input type="date" bind:value={filterFrom} onchange={load} />
					<Input type="date" bind:value={filterTo} onchange={load} />
				</div>
			</Card.Header>
			<Card.Content class="px-0">
				<PageState
					{loading}
					{error}
					empty={events.length === 0}
					onRetry={load}
					emptyIcon={ReceiptTextIcon}
					emptyTitle="還沒有任何交易事件"
					emptyHint="用左側的「新增事件」表單記下第一筆買進、股利或現金進出，庫存就會自動算出來。"
				>
					<div class="scrollbar-thin overflow-x-auto">
						<Table.Root>
							<Table.Header>
								<Table.Row>
									<Table.Head>交易日</Table.Head>
									<Table.Head>類型</Table.Head>
									<Table.Head>標的</Table.Head>
									<Table.Head class="text-right">數量</Table.Head>
									<Table.Head class="text-right">現金變動</Table.Head>
									<Table.Head>來源</Table.Head>
									<Table.Head class="text-right">操作</Table.Head>
								</Table.Row>
							</Table.Header>
							<Table.Body>
								{#each events as ev (ev.id)}
									{@const voided = !!ev.voided_at}
									<Table.Row class={voided ? 'opacity-50' : 'hover:bg-muted/40'}>
										<Table.Cell class="tnum whitespace-nowrap"
											>{formatDate(ev.trade_date)}</Table.Cell
										>
										<Table.Cell>
											<div class="flex flex-col gap-0.5">
												<Badge variant="secondary" class="w-fit font-normal"
													>{eventTypeLabels[ev.event_type]}</Badge
												>
												{#if voided}<span class="text-destructive text-[11px]">已作廢</span>{/if}
											</div>
										</Table.Cell>
										<Table.Cell>
											{#if ev.symbol}<span class="font-medium">{ev.symbol}</span>{:else}<span
													class="text-muted-foreground">—</span
												>{/if}
										</Table.Cell>
										<Table.Cell class="text-right">
											{#if ev.quantity_shares}
												<Quantity shares={ev.quantity_shares} lots={ev.quantity_lots} inline />
											{:else}
												<span class="text-muted-foreground">—</span>
											{/if}
										</Table.Cell>
										<Table.Cell class="text-right">
											<Pnl value={ev.cash_delta} />
											{#if ev.fee_source === 'estimated' && (Number(ev.fee) > 0 || Number(ev.tax) > 0)}
												<div class="text-muted-foreground text-[11px]">費稅估算</div>
											{/if}
										</Table.Cell>
										<Table.Cell>
											<span class="text-muted-foreground text-xs">{ev.source}</span>
											<div class="text-muted-foreground text-[11px]">
												{formatDateTime(ev.created_at)}
											</div>
										</Table.Cell>
										<Table.Cell class="text-right">
											{#if !voided}
												<Button
													variant="ghost"
													size="sm"
													class="text-destructive hover:text-destructive"
													onclick={() => openVoid(ev)}
												>
													<Ban class="size-4" />作廢
												</Button>
											{:else if ev.void_reason}
												<span class="text-muted-foreground text-[11px]" title={ev.void_reason}
													>原因：{ev.void_reason}</span
												>
											{/if}
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
</div>

<!-- 作廢確認 Dialog -->
<Dialog.Root bind:open={voidOpen}>
	<Dialog.Content>
		<Dialog.Header>
			<Dialog.Title>作廢交易事件</Dialog.Title>
			<Dialog.Description>
				作廢不可復原，且不會從歷史中刪除這筆紀錄。若是為了對齊券商資料而需要修正成本或股數，請改用調整事件，不要作廢原始紀錄。
			</Dialog.Description>
		</Dialog.Header>

		{#if voidTarget}
			<div class="bg-muted/50 rounded-lg border p-3 text-sm">
				<div class="flex items-center gap-2">
					<Badge variant="secondary" class="font-normal"
						>{eventTypeLabels[voidTarget.event_type]}</Badge
					>
					{#if voidTarget.symbol}<span class="font-medium">{voidTarget.symbol}</span>{/if}
					<span class="text-muted-foreground">{formatDate(voidTarget.trade_date)}</span>
				</div>
				<p class="text-muted-foreground mt-1 text-xs">
					現金變動 {formatMoney(voidTarget.cash_delta, { sign: true })}
				</p>
			</div>
		{/if}

		<div class="space-y-1.5">
			<Label for="void-reason">作廢原因</Label>
			<Textarea
				id="void-reason"
				rows={3}
				placeholder="例如：重複輸入、輸入錯誤的標的或股數"
				bind:value={voidReason}
			/>
		</div>

		<Dialog.Footer>
			<Button variant="outline" onclick={() => (voidOpen = false)} disabled={voiding}>取消</Button>
			<Button variant="destructive" onclick={confirmVoid} disabled={voiding || !voidReason.trim()}>
				{voiding ? '處理中…' : '確認作廢'}
			</Button>
		</Dialog.Footer>
	</Dialog.Content>
</Dialog.Root>
