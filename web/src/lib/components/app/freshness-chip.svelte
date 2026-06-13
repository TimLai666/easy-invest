<script lang="ts">
	import { onMount } from 'svelte';
	import { freshness } from '$lib/stores/freshness.svelte';
	import {
		Tooltip,
		TooltipTrigger,
		TooltipContent,
		TooltipProvider
	} from '$lib/components/ui/tooltip';
	import { datasetLabel } from '$lib/labels';
	import { formatDate } from '$lib/format';
	import CalendarClockIcon from '@lucide/svelte/icons/calendar-clock';
	import { cn } from '$lib/utils';

	onMount(() => {
		freshness.ensure();
	});

	const stale = $derived(freshness.staleDays);
	// 落後超過 5 個交易日視為過舊（與建議引擎防線一致）
	const tone = $derived(
		stale == null ? 'unknown' : stale >= 5 ? 'bad' : stale >= 2 ? 'warn' : 'ok'
	);
</script>

<TooltipProvider delayDuration={150}>
	<Tooltip>
		<TooltipTrigger>
			<div
				class={cn(
					'inline-flex items-center gap-2 rounded-full border px-3 py-1.5 text-xs font-medium transition-colors',
					tone === 'ok' && 'border-down/30 bg-down-soft/40 text-down',
					tone === 'warn' && 'border-warning/40 bg-warning/10 text-warning-foreground',
					tone === 'bad' && 'border-up/40 bg-up-soft/40 text-up',
					tone === 'unknown' && 'text-muted-foreground bg-muted'
				)}
			>
				<CalendarClockIcon class="size-3.5" />
				{#if freshness.loading && !freshness.loaded}
					<span>讀取資料時間…</span>
				{:else if freshness.marketDataAsOf}
					<span class="tnum">資料 {formatDate(freshness.marketDataAsOf)}</span>
					{#if stale != null}
						<span class="opacity-70">·</span>
						<span>{stale === 0 ? '最新' : `落後 ${stale} 日`}</span>
					{/if}
				{:else}
					<span>尚無市場資料</span>
				{/if}
			</div>
		</TooltipTrigger>
		<TooltipContent class="max-w-xs">
			<div class="space-y-1">
				<p class="font-medium">資料新鮮度</p>
				{#if freshness.items.length}
					{#each freshness.items as item (item.dataset)}
						<div class="flex justify-between gap-4">
							<span>{datasetLabel(item.dataset)}</span>
							<span class="tnum opacity-80">{formatDate(item.latest_data_date) || '—'}</span>
						</div>
					{/each}
				{:else}
					<p class="opacity-80">尚未匯入任何市場資料。</p>
				{/if}
			</div>
		</TooltipContent>
	</Tooltip>
</TooltipProvider>
