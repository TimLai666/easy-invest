<script lang="ts">
	import { onMount } from 'svelte';
	import { reconciliation as reconApi, ApiError } from '$lib/api';
	import type { BrokerSnapshot, ReconciliationRun, ReconciliationDiff, Resolution } from '$lib/api';
	import type { CreateSnapshotBody } from '$lib/api/endpoints';
	import { toDecimal, formatMoney, formatDateTime, unitToShares } from '$lib/format';
	import { diffTypeLabels, resolutionLabels } from '$lib/labels';

	import PageState from '$lib/components/app/page-state.svelte';
	import * as Card from '$lib/components/ui/card';
	import * as Table from '$lib/components/ui/table';
	import * as Select from '$lib/components/ui/select';
	import { Button } from '$lib/components/ui/button';
	import { Badge } from '$lib/components/ui/badge';
	import { Input } from '$lib/components/ui/input';
	import { Label } from '$lib/components/ui/label';
	import { Textarea } from '$lib/components/ui/textarea';
	import { Separator } from '$lib/components/ui/separator';
	import { toast } from '$lib/components/ui/sonner';

	import ScaleIcon from '@lucide/svelte/icons/scale';
	import PlusIcon from '@lucide/svelte/icons/plus';
	import Trash2Icon from '@lucide/svelte/icons/trash-2';
	import UploadIcon from '@lucide/svelte/icons/upload';
	import PlayIcon from '@lucide/svelte/icons/play';
	import CheckCircle2Icon from '@lucide/svelte/icons/check-circle-2';
	import ArrowRightIcon from '@lucide/svelte/icons/arrow-right';
	import ClipboardPasteIcon from '@lucide/svelte/icons/clipboard-paste';

	type Unit = 'share' | 'lot';

	interface PositionRow {
		symbol: string;
		quantity: string;
		unit: Unit;
		brokerAvgCost: string;
	}

	const unitOptions: { value: Unit; label: string }[] = [
		{ value: 'lot', label: '張' },
		{ value: 'share', label: '股' }
	];
	function unitLabel(u: Unit): string {
		return unitOptions.find((o) => o.value === u)?.label ?? '';
	}

	function emptyRow(): PositionRow {
		return { symbol: '', quantity: '', unit: 'lot', brokerAvgCost: '' };
	}

	// 步驟一：表單狀態
	let brokerName = $state('');
	let capturedAt = $state('');
	let rows = $state<PositionRow[]>([emptyRow()]);
	let csvText = $state('');
	let submitting = $state(false);

	// 步驟二：快照清單
	let snapshots = $state<BrokerSnapshot[]>([]);
	let loading = $state(true);
	let error = $state<unknown>(null);

	// 步驟三：對帳結果
	let run = $state<ReconciliationRun | null>(null);
	let activeSnapshotId = $state<string | null>(null);
	let runningId = $state<string | null>(null);
	let resolvingId = $state<string | null>(null);

	async function loadSnapshots() {
		loading = true;
		error = null;
		try {
			snapshots = await reconApi.listSnapshots();
		} catch (e) {
			error = e;
		} finally {
			loading = false;
		}
	}
	onMount(loadSnapshots);

	function addRow() {
		rows = [...rows, emptyRow()];
	}
	function removeRow(i: number) {
		rows = rows.filter((_, idx) => idx !== i);
		if (rows.length === 0) rows = [emptyRow()];
	}

	function parseCsv() {
		const lines = csvText
			.split(/\r?\n/)
			.map((l) => l.trim())
			.filter((l) => l.length > 0);
		if (lines.length === 0) {
			toast.error('沒有可解析的內容');
			return;
		}
		// 第一行若像標題則略過
		const first = lines[0].toLowerCase();
		const startIdx = /symbol|代號|股數|quantity/.test(first) ? 1 : 0;
		const parsed: PositionRow[] = [];
		for (let i = startIdx; i < lines.length; i++) {
			const cols = lines[i].split(',').map((c) => c.trim());
			const symbol = cols[0] ?? '';
			if (!symbol) continue;
			parsed.push({
				symbol,
				quantity: cols[1] ?? '',
				unit: 'share',
				brokerAvgCost: cols[2] ?? ''
			});
		}
		if (parsed.length === 0) {
			toast.error('解析後沒有有效資料，格式應為 symbol,quantity_shares,avg_cost');
			return;
		}
		rows = parsed;
		csvText = '';
		toast.success(`已解析 ${parsed.length} 筆持股（單位為股）`);
	}

	function validRows(): PositionRow[] {
		return rows.filter((r) => r.symbol.trim() && toDecimal(r.quantity) !== null);
	}

	async function submitSnapshot(e: SubmitEvent) {
		e.preventDefault();
		if (!brokerName.trim()) {
			toast.error('請輸入券商名稱');
			return;
		}
		const vrows = validRows();
		if (vrows.length === 0) {
			toast.error('請至少輸入一筆有效持股（標的與股數）');
			return;
		}
		const positions: CreateSnapshotBody['positions'] = [];
		for (const r of vrows) {
			const shares = unitToShares(r.quantity, r.unit);
			if (shares === null) {
				toast.error(`標的 ${r.symbol} 的股數格式有誤`);
				return;
			}
			const avgCost = toDecimal(r.brokerAvgCost);
			positions.push({
				symbol: r.symbol.trim(),
				quantity_shares: shares,
				broker_avg_cost: avgCost !== null ? avgCost.toString() : null
			});
		}

		submitting = true;
		try {
			await reconApi.createSnapshot({
				broker_name: brokerName.trim(),
				captured_at: capturedAt ? new Date(capturedAt).toISOString() : null,
				positions
			});
			toast.success('已匯入券商庫存快照');
			// 重置表單
			brokerName = '';
			capturedAt = '';
			rows = [emptyRow()];
			csvText = '';
			await loadSnapshots();
		} catch (err) {
			toast.error(err instanceof ApiError ? err.message : '匯入失敗，請稍後再試');
		} finally {
			submitting = false;
		}
	}

	async function startRun(snapshotId: string) {
		runningId = snapshotId;
		try {
			const result = await reconApi.createRun(snapshotId);
			run = result;
			activeSnapshotId = snapshotId;
			toast.success(`對帳完成，找出 ${result.diffs?.length ?? 0} 筆差異`);
		} catch (err) {
			toast.error(err instanceof ApiError ? err.message : '對帳失敗，請稍後再試');
		} finally {
			runningId = null;
		}
	}

	async function resolve(diff: ReconciliationDiff, resolution: Exclude<Resolution, 'pending'>) {
		resolvingId = diff.id;
		try {
			const updated = await reconApi.resolveDiff(diff.id, resolution);
			if (run?.diffs) {
				run = {
					...run,
					diffs: run.diffs.map((d) => (d.id === updated.id ? updated : d))
				};
			}
			toast.success('已更新差異處理方式');
		} catch (err) {
			toast.error(err instanceof ApiError ? err.message : '處理失敗，請稍後再試');
		} finally {
			resolvingId = null;
		}
	}

	const diffs = $derived(run?.diffs ?? []);
	const allResolved = $derived(diffs.length > 0 && diffs.every((d) => d.resolution !== 'pending'));
	const activeSnapshot = $derived(snapshots.find((s) => s.id === activeSnapshotId) ?? null);

	function snapshotLabel(s: BrokerSnapshot): string {
		return `${s.broker_name}（${formatDateTime(s.captured_at)}）`;
	}
</script>

<div class="space-y-6">
	<div>
		<h1 class="text-2xl font-semibold tracking-tight">券商對帳精靈</h1>
		<p class="text-muted-foreground mt-1 text-sm">
			匯入券商端的庫存快照，與系統由交易流水計算出的部位逐筆比對；差異不會直接覆蓋歷史，只能透過可追蹤的調整事件修正。
		</p>
	</div>

	<!-- 步驟一：上傳券商庫存 -->
	<Card.Root>
		<Card.Header>
			<div class="flex items-center gap-3">
				<span
					class="bg-primary text-primary-foreground flex size-7 shrink-0 items-center justify-center rounded-full text-sm font-semibold"
					>1</span
				>
				<div>
					<Card.Title>上傳券商庫存</Card.Title>
					<Card.Description>填入券商名稱、資料時點與各標的持股，或直接貼上 CSV。</Card.Description>
				</div>
			</div>
		</Card.Header>
		<Card.Content>
			<form class="space-y-6" onsubmit={submitSnapshot}>
				<div class="grid gap-4 sm:grid-cols-2">
					<div class="space-y-1.5">
						<Label for="broker-name">券商名稱</Label>
						<Input id="broker-name" bind:value={brokerName} placeholder="例如：富邦證券" />
					</div>
					<div class="space-y-1.5">
						<Label for="captured-at">資料時點</Label>
						<Input id="captured-at" type="datetime-local" bind:value={capturedAt} />
						<p class="text-muted-foreground text-xs">
							券商這份庫存的截止時間，留空則以匯入當下為準。
						</p>
					</div>
				</div>

				<div class="space-y-3">
					<div class="flex items-center justify-between">
						<Label>持股明細</Label>
						<Button type="button" variant="outline" size="sm" onclick={addRow}>
							<PlusIcon class="size-4" />新增一列
						</Button>
					</div>
					<div class="scrollbar-thin overflow-x-auto rounded-lg border">
						<Table.Root>
							<Table.Header>
								<Table.Row>
									<Table.Head class="min-w-32">標的代號</Table.Head>
									<Table.Head class="min-w-28">數量</Table.Head>
									<Table.Head class="min-w-24">單位</Table.Head>
									<Table.Head class="min-w-32">券商均價</Table.Head>
									<Table.Head class="w-12"></Table.Head>
								</Table.Row>
							</Table.Header>
							<Table.Body>
								{#each rows as row, i (i)}
									<Table.Row>
										<Table.Cell>
											<Input bind:value={row.symbol} placeholder="2330" class="h-9" />
										</Table.Cell>
										<Table.Cell>
											<Input
												bind:value={row.quantity}
												inputmode="decimal"
												placeholder="0"
												class="h-9"
											/>
										</Table.Cell>
										<Table.Cell>
											<Select.Root type="single" bind:value={row.unit}>
												<Select.Trigger class="h-9 w-full">
													{unitLabel(row.unit)}
												</Select.Trigger>
												<Select.Content>
													{#each unitOptions as opt (opt.value)}
														<Select.Item value={opt.value}>{opt.label}</Select.Item>
													{/each}
												</Select.Content>
											</Select.Root>
										</Table.Cell>
										<Table.Cell>
											<Input
												bind:value={row.brokerAvgCost}
												inputmode="decimal"
												placeholder="可空"
												class="h-9"
											/>
										</Table.Cell>
										<Table.Cell>
											<Button
												type="button"
												variant="ghost"
												size="icon"
												class="size-9"
												onclick={() => removeRow(i)}
												aria-label="刪除此列"
											>
												<Trash2Icon class="text-muted-foreground size-4" />
											</Button>
										</Table.Cell>
									</Table.Row>
								{/each}
							</Table.Body>
						</Table.Root>
					</div>
					<p class="text-muted-foreground text-xs">
						一張台股等於 1000 股；請依券商表示方式選擇單位。
					</p>
				</div>

				<Separator />

				<div class="space-y-2">
					<Label for="csv-paste">貼上 CSV（選用）</Label>
					<Textarea
						id="csv-paste"
						bind:value={csvText}
						rows={4}
						placeholder={'symbol,quantity_shares,avg_cost\n2330,1000,580.5\n0050,2000,135'}
						class="font-mono text-xs"
					/>
					<div class="flex items-center justify-between gap-3">
						<p class="text-muted-foreground text-xs">
							格式：symbol,quantity_shares,avg_cost，每行一筆，第一行可為標題。解析後單位為「股」。
						</p>
						<Button
							type="button"
							variant="outline"
							size="sm"
							onclick={parseCsv}
							disabled={!csvText.trim()}
						>
							<ClipboardPasteIcon class="size-4" />解析並填入
						</Button>
					</div>
				</div>

				<div class="flex justify-end">
					<Button type="submit" disabled={submitting}>
						<UploadIcon class="size-4" />{submitting ? '匯入中…' : '匯入快照'}
					</Button>
				</div>
			</form>
		</Card.Content>
	</Card.Root>

	<!-- 步驟二：快照清單 -->
	<Card.Root>
		<Card.Header>
			<div class="flex items-center gap-3">
				<span
					class="bg-primary text-primary-foreground flex size-7 shrink-0 items-center justify-center rounded-full text-sm font-semibold"
					>2</span
				>
				<div>
					<Card.Title>選擇快照開始對帳</Card.Title>
					<Card.Description>每筆快照可獨立建立一次對帳，比對結果顯示在下方步驟三。</Card.Description
					>
				</div>
			</div>
		</Card.Header>
		<Card.Content class="px-0">
			<PageState
				{loading}
				{error}
				empty={snapshots.length === 0}
				onRetry={loadSnapshots}
				emptyIcon={ScaleIcon}
				emptyTitle="尚未匯入券商資料"
				emptyHint="請先用上方「上傳券商庫存」表單匯入一份券商快照，匯入後就能在這裡開始對帳。"
			>
				<div class="scrollbar-thin overflow-x-auto">
					<Table.Root>
						<Table.Header>
							<Table.Row>
								<Table.Head>券商</Table.Head>
								<Table.Head>資料時點</Table.Head>
								<Table.Head>建立時間</Table.Head>
								<Table.Head class="text-right">操作</Table.Head>
							</Table.Row>
						</Table.Header>
						<Table.Body>
							{#each snapshots as s (s.id)}
								<Table.Row class="hover:bg-muted/40">
									<Table.Cell class="font-medium">{s.broker_name}</Table.Cell>
									<Table.Cell class="text-muted-foreground"
										>{formatDateTime(s.captured_at)}</Table.Cell
									>
									<Table.Cell class="text-muted-foreground"
										>{formatDateTime(s.created_at)}</Table.Cell
									>
									<Table.Cell class="text-right">
										<Button
											variant={activeSnapshotId === s.id ? 'secondary' : 'outline'}
											size="sm"
											onclick={() => startRun(s.id)}
											disabled={runningId !== null}
										>
											<PlayIcon class="size-4" />
											{runningId === s.id ? '對帳中…' : '開始對帳'}
										</Button>
									</Table.Cell>
								</Table.Row>
							{/each}
						</Table.Body>
					</Table.Root>
				</div>
			</PageState>
		</Card.Content>
	</Card.Root>

	<!-- 步驟三：差異 -->
	<Card.Root>
		<Card.Header>
			<div class="flex items-center gap-3">
				<span
					class="bg-primary text-primary-foreground flex size-7 shrink-0 items-center justify-center rounded-full text-sm font-semibold"
					>3</span
				>
				<div>
					<Card.Title>差異與處理</Card.Title>
					<Card.Description>
						{#if activeSnapshot}
							比對標的：{snapshotLabel(activeSnapshot)}
						{:else}
							在步驟二按下「開始對帳」後，這裡會列出系統與券商之間的差異。
						{/if}
					</Card.Description>
				</div>
			</div>
		</Card.Header>
		<Card.Content class="space-y-4">
			{#if !run}
				<div
					class="bg-muted/30 text-muted-foreground rounded-lg border border-dashed p-8 text-center text-sm"
				>
					尚未執行對帳。
				</div>
			{:else if diffs.length === 0}
				<div
					class="text-up flex flex-col items-center gap-2 rounded-lg border border-dashed p-8 text-center"
				>
					<CheckCircle2Icon class="size-8" />
					<p class="font-medium">沒有發現差異</p>
					<p class="text-muted-foreground text-sm">系統部位與此份券商快照完全一致。</p>
				</div>
			{:else}
				{#if allResolved}
					<div class="border-up/40 bg-up/5 text-up flex items-center gap-3 rounded-lg border p-4">
						<CheckCircle2Icon class="size-5 shrink-0" />
						<div>
							<p class="font-medium">所有差異都已處理完成</p>
							<p class="text-muted-foreground text-sm">
								本次對帳的 {diffs.length} 筆差異皆已標記處理方式。
							</p>
						</div>
					</div>
				{/if}

				<div class="space-y-3">
					{#each diffs as diff (diff.id)}
						<div class="rounded-lg border p-4">
							<div class="flex flex-wrap items-center justify-between gap-2">
								<div class="flex items-center gap-2">
									<Badge variant="outline">{diffTypeLabels[diff.diff_type] ?? diff.diff_type}</Badge
									>
									{#if diff.symbol}<span class="font-medium">{diff.symbol}</span>{/if}
								</div>
								{#if diff.resolution !== 'pending'}
									<Badge variant="secondary">{resolutionLabels[diff.resolution]}</Badge>
								{:else}
									<Badge variant="outline" class="text-muted-foreground"
										>{resolutionLabels.pending}</Badge
									>
								{/if}
							</div>

							<div class="mt-3 grid gap-3 sm:grid-cols-2">
								<div class="bg-muted/30 rounded-md p-3">
									<p class="text-muted-foreground text-xs">系統值</p>
									<p class="tnum mt-0.5 font-medium">
										{diff.internal_value != null
											? formatMoney(diff.internal_value, { dp: 4 })
											: '—'}
									</p>
								</div>
								<div class="bg-muted/30 rounded-md p-3">
									<p class="text-muted-foreground text-xs">券商值</p>
									<p class="tnum mt-0.5 font-medium">
										{diff.broker_value != null ? formatMoney(diff.broker_value, { dp: 4 }) : '—'}
									</p>
								</div>
							</div>

							{#if diff.resolution === 'pending'}
								<div class="mt-3 flex flex-wrap gap-2">
									<Button
										variant="default"
										size="sm"
										onclick={() => resolve(diff, 'adjusted')}
										disabled={resolvingId !== null}
									>
										建立調整事件
									</Button>
									<Button
										variant="outline"
										size="sm"
										onclick={() => resolve(diff, 'accepted_as_is')}
										disabled={resolvingId !== null}
									>
										接受差異
									</Button>
									<Button
										variant="outline"
										size="sm"
										onclick={() => resolve(diff, 'fixed_input')}
										disabled={resolvingId !== null}
									>
										修正輸入
									</Button>
								</div>
							{:else}
								<div class="text-muted-foreground mt-3 flex flex-wrap items-center gap-3 text-sm">
									<span>處理方式：{resolutionLabels[diff.resolution]}</span>
									{#if diff.resolved_at}
										<span>·　{formatDateTime(diff.resolved_at)}</span>
									{/if}
									{#if diff.adjustment_event_id}
										<a
											href="/transactions?event={diff.adjustment_event_id}"
											class="text-primary inline-flex items-center gap-1 hover:underline"
										>
											已建調整事件 <ArrowRightIcon class="size-3.5" />
										</a>
									{/if}
								</div>
							{/if}
						</div>
					{/each}
				</div>
			{/if}
		</Card.Content>
	</Card.Root>
</div>
