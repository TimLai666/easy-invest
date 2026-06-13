<script lang="ts">
	import { onMount } from 'svelte';
	import { settings as settingsApi, ApiError } from '$lib/api';
	import type { Settings, Strategy } from '$lib/api';
	import { toDecimal, formatPercent } from '$lib/format';
	import { riskProfileLabels } from '$lib/labels';
	import Decimal from 'decimal.js';

	import PageState from '$lib/components/app/page-state.svelte';
	import * as Card from '$lib/components/ui/card';
	import * as Table from '$lib/components/ui/table';
	import * as Select from '$lib/components/ui/select';
	import { Button } from '$lib/components/ui/button';
	import { Input } from '$lib/components/ui/input';
	import { Label } from '$lib/components/ui/label';
	import { Switch } from '$lib/components/ui/switch';
	import { Badge } from '$lib/components/ui/badge';
	import { Skeleton } from '$lib/components/ui/skeleton';
	import { toast } from '$lib/components/ui/sonner';

	import SettingsIcon from '@lucide/svelte/icons/settings';
	import PercentIcon from '@lucide/svelte/icons/percent';
	import TargetIcon from '@lucide/svelte/icons/target';
	import LayersIcon from '@lucide/svelte/icons/layers';
	import PlusIcon from '@lucide/svelte/icons/plus';
	import Trash2Icon from '@lucide/svelte/icons/trash-2';
	import TriangleAlertIcon from '@lucide/svelte/icons/triangle-alert';

	let data = $state<Settings | null>(null);
	let strategies = $state<Strategy[]>([]);
	let loading = $state(true);
	let error = $state<unknown>(null);
	let saving = $state(false);

	// 費率與交易表單欄位（數值以字串保存）
	let feeRate = $state('');
	let feeDiscount = $state('');
	let feeMinimum = $state('');
	let dividendTransferFee = $state('');
	let cashBuffer = $state('');
	let minTradeAmount = $state('');
	let rebalanceBand = $state('');
	let preferWholeLot = $state(false);
	let riskProfile = $state('balanced');

	// 目標權重列（symbol / weight 字串 0~1）
	type WeightRow = { id: number; symbol: string; weight: string };
	let weightRows = $state<WeightRow[]>([]);
	let rowSeq = 0;

	const riskOptions = ['conservative', 'balanced', 'aggressive'];

	function hydrate(s: Settings) {
		feeRate = s.fee_rate ?? '';
		feeDiscount = s.fee_discount ?? '';
		feeMinimum = s.fee_minimum ?? '';
		dividendTransferFee = s.dividend_transfer_fee ?? '';
		cashBuffer = s.cash_buffer ?? '';
		minTradeAmount = s.min_trade_amount ?? '';
		rebalanceBand = s.rebalance_band ?? '';
		preferWholeLot = !!s.prefer_whole_lot;
		riskProfile = s.risk_profile || 'balanced';
		weightRows = Object.entries(s.target_weights ?? {}).map(([symbol, weight]) => ({
			id: rowSeq++,
			symbol,
			weight: String(weight)
		}));
	}

	async function load() {
		loading = true;
		error = null;
		try {
			const [s, strats] = await Promise.all([settingsApi.get(), settingsApi.strategies()]);
			data = s;
			strategies = strats;
			hydrate(s);
		} catch (e) {
			error = e;
		} finally {
			loading = false;
		}
	}
	onMount(load);

	function addWeightRow() {
		weightRows = [...weightRows, { id: rowSeq++, symbol: '', weight: '' }];
	}
	function removeWeightRow(id: number) {
		weightRows = weightRows.filter((r) => r.id !== id);
	}

	// 權重總和（用 decimal.js 加總，禁止 float 運算）
	const weightSum = $derived(
		weightRows.reduce((s, r) => s.plus(toDecimal(r.weight) ?? new Decimal(0)), new Decimal(0))
	);
	const sumIsValid = $derived(weightSum.minus(1).abs().lessThanOrEqualTo(new Decimal('0.005')));

	function diffNumeric(field: keyof Settings, current: string): boolean {
		if (!data) return current.trim() !== '';
		return String(data[field] ?? '') !== current.trim();
	}

	async function save(e: SubmitEvent) {
		e.preventDefault();
		if (!data || saving) return;
		const body: Record<string, unknown> = {};

		// 只送有改的數值欄位（皆以字串送出）
		const numericMap: [keyof Settings, string][] = [
			['fee_rate', feeRate],
			['fee_discount', feeDiscount],
			['fee_minimum', feeMinimum],
			['dividend_transfer_fee', dividendTransferFee],
			['cash_buffer', cashBuffer],
			['min_trade_amount', minTradeAmount],
			['rebalance_band', rebalanceBand]
		];
		for (const [field, val] of numericMap) {
			if (diffNumeric(field, val)) body[field] = val.trim();
		}

		if (preferWholeLot !== data.prefer_whole_lot) body.prefer_whole_lot = preferWholeLot;
		if (riskProfile !== data.risk_profile) body.risk_profile = riskProfile;

		// 目標權重永遠以物件送出（symbol -> weight 字串）
		const weights: Record<string, string> = {};
		for (const r of weightRows) {
			const sym = r.symbol.trim();
			if (!sym) continue;
			weights[sym] = r.weight.trim() || '0';
		}
		const currentWeights: Record<string, string> = {};
		for (const [k, v] of Object.entries(data.target_weights ?? {})) currentWeights[k] = String(v);
		if (JSON.stringify(weights) !== JSON.stringify(currentWeights)) {
			body.target_weights = weights;
		}

		if (Object.keys(body).length === 0) {
			toast.info('沒有變更需要儲存');
			return;
		}

		saving = true;
		try {
			const updated = await settingsApi.update(body);
			data = updated;
			hydrate(updated);
			toast.success('設定已儲存');
		} catch (err) {
			toast.error(err instanceof ApiError ? err.message : '儲存失敗，請稍後再試。');
		} finally {
			saving = false;
		}
	}
</script>

<svelte:head><title>設定 · Easy Invest</title></svelte:head>

<div class="space-y-6">
	<div>
		<h1 class="text-2xl font-semibold tracking-tight">設定</h1>
		<p class="text-muted-foreground mt-1 text-sm">
			調整費率、交易規則、風險偏好與再平衡目標權重。這些設定會套用到建議與回測計算。
		</p>
	</div>

	<PageState {loading} {error} onRetry={load} emptyIcon={SettingsIcon} emptyTitle="尚無設定">
		{#snippet skeleton()}
			<div class="space-y-6">
				<Skeleton class="h-72 w-full rounded-xl" />
				<Skeleton class="h-64 w-full rounded-xl" />
				<Skeleton class="h-40 w-full rounded-xl" />
			</div>
		{/snippet}

		<form class="space-y-6" onsubmit={save}>
			<!-- 費率與交易 -->
			<Card.Root>
				<Card.Header class="flex flex-row items-center gap-2">
					<PercentIcon class="size-5 opacity-70" />
					<div>
						<Card.Title>費率與交易</Card.Title>
						<Card.Description
							>手續費、稅費與下單參數，皆為數值。費率類欄位以小數比例表示（例如 0.001425 表示
							0.1425%）。</Card.Description
						>
					</div>
				</Card.Header>
				<Card.Content class="space-y-6">
					<div class="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
						<div class="space-y-2">
							<Label for="fee_rate">手續費率</Label>
							<Input
								id="fee_rate"
								inputmode="decimal"
								bind:value={feeRate}
								placeholder="0.001425"
							/>
							<p class="text-muted-foreground text-xs">官方公定費率，通常為 0.001425。</p>
						</div>
						<div class="space-y-2">
							<Label for="fee_discount">手續費折數</Label>
							<Input
								id="fee_discount"
								inputmode="decimal"
								bind:value={feeDiscount}
								placeholder="0.6"
							/>
							<p class="text-muted-foreground text-xs">券商折扣，例如 0.6 表示打 6 折。</p>
						</div>
						<div class="space-y-2">
							<Label for="fee_minimum">最低手續費</Label>
							<Input
								id="fee_minimum"
								inputmode="decimal"
								bind:value={feeMinimum}
								placeholder="20"
							/>
							<p class="text-muted-foreground text-xs">單筆手續費下限（元）。</p>
						</div>
						<div class="space-y-2">
							<Label for="dividend_transfer_fee">股利匯費</Label>
							<Input
								id="dividend_transfer_fee"
								inputmode="decimal"
								bind:value={dividendTransferFee}
								placeholder="10"
							/>
							<p class="text-muted-foreground text-xs">現金股利入帳匯費（元）。</p>
						</div>
						<div class="space-y-2">
							<Label for="cash_buffer">現金緩衝</Label>
							<Input
								id="cash_buffer"
								inputmode="decimal"
								bind:value={cashBuffer}
								placeholder="0.05"
							/>
							<p class="text-muted-foreground text-xs">建議引擎不會動用的現金金額（元）。</p>
						</div>
						<div class="space-y-2">
							<Label for="min_trade_amount">最小交易金額</Label>
							<Input
								id="min_trade_amount"
								inputmode="decimal"
								bind:value={minTradeAmount}
								placeholder="1000"
							/>
							<p class="text-muted-foreground text-xs">低於此金額的調整建議會被忽略（元）。</p>
						</div>
						<div class="space-y-2">
							<Label for="rebalance_band">再平衡容忍帶</Label>
							<Input
								id="rebalance_band"
								inputmode="decimal"
								bind:value={rebalanceBand}
								placeholder="0.05"
							/>
							<p class="text-muted-foreground text-xs">偏離目標超過此比例才觸發調整（0~1）。</p>
						</div>
						<div class="space-y-2">
							<Label for="risk_profile">風險偏好</Label>
							<Select.Root type="single" bind:value={riskProfile}>
								<Select.Trigger id="risk_profile" class="w-full">
									{riskProfile ? (riskProfileLabels[riskProfile] ?? riskProfile) : '選擇風險偏好'}
								</Select.Trigger>
								<Select.Content>
									{#each riskOptions as opt (opt)}
										<Select.Item value={opt} label={riskProfileLabels[opt]}
											>{riskProfileLabels[opt]}</Select.Item
										>
									{/each}
								</Select.Content>
							</Select.Root>
							<p class="text-muted-foreground text-xs">影響建議的積極程度與部位上限。</p>
						</div>
					</div>

					<div class="flex items-center justify-between rounded-lg border p-4">
						<div class="space-y-0.5">
							<Label for="prefer_whole_lot">偏好整張交易</Label>
							<p class="text-muted-foreground text-xs">
								開啟後建議盡量以整張（1000 股）為單位，減少零股下單。
							</p>
						</div>
						<Switch id="prefer_whole_lot" bind:checked={preferWholeLot} />
					</div>
				</Card.Content>
			</Card.Root>

			<!-- 目標權重 -->
			<Card.Root>
				<Card.Header class="flex flex-row items-center justify-between gap-2">
					<div class="flex items-center gap-2">
						<TargetIcon class="size-5 opacity-70" />
						<div>
							<Card.Title>目標權重</Card.Title>
							<Card.Description
								>各標的的目標配置比例（0~1）。再平衡建議會以此為基準。</Card.Description
							>
						</div>
					</div>
					<Button type="button" variant="outline" size="sm" onclick={addWeightRow}>
						<PlusIcon class="size-4" />新增標的
					</Button>
				</Card.Header>
				<Card.Content class="space-y-4">
					{#if weightRows.length === 0}
						<div class="bg-muted/30 rounded-lg border border-dashed p-8 text-center">
							<p class="text-muted-foreground text-sm">尚未設定任何目標權重。</p>
							<Button type="button" variant="outline" size="sm" class="mt-3" onclick={addWeightRow}>
								<PlusIcon class="size-4" />新增第一個標的
							</Button>
						</div>
					{:else}
						<div class="overflow-x-auto">
							<Table.Root>
								<Table.Header>
									<Table.Row>
										<Table.Head>標的代號</Table.Head>
										<Table.Head class="w-40">目標權重（0~1）</Table.Head>
										<Table.Head class="w-28 text-right">換算 %</Table.Head>
										<Table.Head class="w-12"></Table.Head>
									</Table.Row>
								</Table.Header>
								<Table.Body>
									{#each weightRows as row (row.id)}
										<Table.Row>
											<Table.Cell>
												<Input bind:value={row.symbol} placeholder="例如 0050" class="max-w-40" />
											</Table.Cell>
											<Table.Cell>
												<Input inputmode="decimal" bind:value={row.weight} placeholder="0.3" />
											</Table.Cell>
											<Table.Cell class="tnum text-muted-foreground text-right text-sm">
												{toDecimal(row.weight) ? formatPercent(row.weight) : '—'}
											</Table.Cell>
											<Table.Cell class="text-right">
												<Button
													type="button"
													variant="ghost"
													size="icon"
													class="text-muted-foreground hover:text-destructive"
													onclick={() => removeWeightRow(row.id)}
													aria-label="移除"
												>
													<Trash2Icon class="size-4" />
												</Button>
											</Table.Cell>
										</Table.Row>
									{/each}
								</Table.Body>
							</Table.Root>
						</div>

						<div class="flex items-center justify-between rounded-lg border p-4">
							<span class="text-sm font-medium">權重總和</span>
							<div class="flex items-center gap-2">
								<Badge variant={sumIsValid ? 'secondary' : 'outline'} class="tnum">
									{formatPercent(weightSum.toString())}
								</Badge>
								{#if !sumIsValid}
									<span class="text-amber-600 dark:text-amber-500 flex items-center gap-1 text-xs">
										<TriangleAlertIcon class="size-3.5" />總和未接近 100%，建議調整為合計 1
									</span>
								{/if}
							</div>
						</div>
						<p class="text-muted-foreground text-xs">
							權重不為 100% 時仍可儲存，後端會進行最終驗證。
						</p>
					{/if}
				</Card.Content>
			</Card.Root>

			<div class="flex items-center justify-end gap-3">
				<Button
					type="button"
					variant="outline"
					disabled={saving}
					onclick={() => data && hydrate(data)}
				>
					還原
				</Button>
				<Button type="submit" disabled={saving}>{saving ? '儲存中…' : '儲存設定'}</Button>
			</div>
		</form>

		<!-- 可用策略 -->
		<Card.Root>
			<Card.Header class="flex flex-row items-center gap-2">
				<LayersIcon class="size-5 opacity-70" />
				<div>
					<Card.Title>可用策略</Card.Title>
					<Card.Description>目前系統提供的建議與回測策略。</Card.Description>
				</div>
			</Card.Header>
			<Card.Content class="px-0">
				{#if strategies.length === 0}
					<p class="text-muted-foreground px-6 py-8 text-center text-sm">尚無可用策略。</p>
				{:else}
					<div class="overflow-x-auto">
						<Table.Root>
							<Table.Header>
								<Table.Row>
									<Table.Head>策略名稱</Table.Head>
									<Table.Head class="w-28">版本</Table.Head>
									<Table.Head>說明</Table.Head>
								</Table.Row>
							</Table.Header>
							<Table.Body>
								{#each strategies as s (s.id)}
									<Table.Row>
										<Table.Cell class="font-medium">{s.name}</Table.Cell>
										<Table.Cell>
											<Badge variant="secondary" class="font-normal">{s.version}</Badge>
										</Table.Cell>
										<Table.Cell class="text-muted-foreground text-sm"
											>{s.description || '—'}</Table.Cell
										>
									</Table.Row>
								{/each}
							</Table.Body>
						</Table.Root>
					</div>
				{/if}
			</Card.Content>
		</Card.Root>
	</PageState>
</div>
