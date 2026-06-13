<script lang="ts">
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { recommendations as recoApi, ApiError } from '$lib/api';
	import type { RecommendationRun } from '$lib/api';
	import { formatDateTime } from '$lib/format';
	import { toast } from '$lib/components/ui/sonner';

	import PageState from '$lib/components/app/page-state.svelte';
	import Disclaimer from '$lib/components/app/disclaimer.svelte';
	import * as Card from '$lib/components/ui/card';
	import * as Table from '$lib/components/ui/table';
	import { Button } from '$lib/components/ui/button';
	import { Badge } from '$lib/components/ui/badge';

	import LightbulbIcon from '@lucide/svelte/icons/lightbulb';
	import SparklesIcon from '@lucide/svelte/icons/sparkles';
	import Loader2Icon from '@lucide/svelte/icons/loader-2';
	import ChevronRightIcon from '@lucide/svelte/icons/chevron-right';

	let runs = $state<RecommendationRun[]>([]);
	let loading = $state(true);
	let error = $state<unknown>(null);
	let generating = $state(false);

	async function load() {
		loading = true;
		error = null;
		try {
			runs = await recoApi.list(20);
		} catch (e) {
			error = e;
		} finally {
			loading = false;
		}
	}
	onMount(load);

	async function generate() {
		if (generating) return;
		generating = true;
		try {
			const run = await recoApi.run();
			toast.success('已產生新建議');
			await goto(`/recommendations/${run.id}`);
		} catch (e) {
			toast.error(e instanceof ApiError ? e.message : '產生建議失敗，請稍後再試');
			generating = false;
		}
	}

	const latest = $derived(runs[0] ?? null);

	function statusVariant(status: string): 'default' | 'secondary' | 'outline' {
		if (status === 'completed' || status === 'ready') return 'default';
		if (status === 'failed' || status === 'error') return 'outline';
		return 'secondary';
	}

	const statusLabels: Record<string, string> = {
		completed: '完成',
		ready: '完成',
		pending: '計算中',
		running: '計算中',
		failed: '失敗',
		error: '失敗',
		expired: '已過期'
	};
	function statusLabel(status: string): string {
		return statusLabels[status] ?? status;
	}
</script>

<div class="space-y-6">
	<!-- 觸發卡片 -->
	<Card.Root>
		<Card.Header class="flex flex-row items-start justify-between gap-4">
			<div class="space-y-1.5">
				<Card.Title class="flex items-center gap-2">
					<LightbulbIcon class="text-primary size-5" />投資建議
				</Card.Title>
				<Card.Description>
					依據你目前的庫存、設定中的目標權重與最新日終市場資料，產生可追蹤、可解釋的調整建議。
				</Card.Description>
			</div>
			<Button onclick={generate} disabled={generating}>
				{#if generating}
					<Loader2Icon class="size-4 animate-spin" />產生中…
				{:else}
					<SparklesIcon class="size-4" />產生新建議
				{/if}
			</Button>
		</Card.Header>
		<Card.Footer class="text-muted-foreground text-xs">
			將使用「設定」頁中的目標權重與再平衡區間進行計算；本系統僅提供參考資訊，不代為下單。
		</Card.Footer>
	</Card.Root>

	<!-- 歷史 run 清單 -->
	<Card.Root>
		<Card.Header>
			<Card.Title>建議紀錄</Card.Title>
			<Card.Description>最近 {runs.length} 份建議，點擊任一筆檢視完整內容</Card.Description>
		</Card.Header>
		<Card.Content class="px-0">
			<PageState
				{loading}
				{error}
				empty={runs.length === 0}
				onRetry={load}
				emptyIcon={LightbulbIcon}
				emptyTitle="還沒有任何建議紀錄"
				emptyHint="點選上方「產生新建議」，系統會依你的庫存與目標權重計算第一份調整建議。"
			>
				{#snippet emptyAction()}
					<Button onclick={generate} disabled={generating}>
						{#if generating}
							<Loader2Icon class="size-4 animate-spin" />產生中…
						{:else}
							<SparklesIcon class="size-4" />產生第一份建議
						{/if}
					</Button>
				{/snippet}

				<div class="scrollbar-thin overflow-x-auto">
					<Table.Root>
						<Table.Header>
							<Table.Row>
								<Table.Head>建立時間</Table.Head>
								<Table.Head>策略</Table.Head>
								<Table.Head>市場資料日期</Table.Head>
								<Table.Head>狀態</Table.Head>
								<Table.Head class="w-10"></Table.Head>
							</Table.Row>
						</Table.Header>
						<Table.Body>
							{#each runs as run (run.id)}
								<Table.Row
									class="hover:bg-muted/40 cursor-pointer"
									onclick={() => goto(`/recommendations/${run.id}`)}
								>
									<Table.Cell class="font-medium">{formatDateTime(run.created_at)}</Table.Cell>
									<Table.Cell>
										<div class="flex flex-col">
											<span>{run.strategy?.name ?? '—'}</span>
											{#if run.strategy?.version}
												<span class="text-muted-foreground text-xs">v{run.strategy.version}</span>
											{/if}
										</div>
									</Table.Cell>
									<Table.Cell class="tnum text-muted-foreground"
										>{run.market_data_as_of || '—'}</Table.Cell
									>
									<Table.Cell>
										<Badge variant={statusVariant(run.status)} class="font-normal"
											>{statusLabel(run.status)}</Badge
										>
									</Table.Cell>
									<Table.Cell class="text-muted-foreground text-right">
										<ChevronRightIcon class="size-4" />
									</Table.Cell>
								</Table.Row>
							{/each}
						</Table.Body>
					</Table.Root>
				</div>
			</PageState>
		</Card.Content>
	</Card.Root>

	<Disclaimer
		text={latest?.disclaimer}
		marketDataAsOf={latest?.market_data_as_of ?? null}
		portfolioAsOf={latest?.portfolio_as_of ?? null}
	/>
</div>
