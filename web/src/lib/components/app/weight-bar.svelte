<script lang="ts">
	import { toDecimal } from '$lib/format';
	import { cn } from '$lib/utils';

	// current/target 為 0~1 比例字串
	let {
		current,
		target,
		band = null,
		class: className = ''
	}: {
		current: string | number | null | undefined;
		target: string | number | null | undefined;
		band?: string | number | null;
		class?: string;
	} = $props();

	const cur = $derived(Number(toDecimal(current)?.toString() ?? 0) * 100);
	const tgt = $derived(
		toDecimal(target) != null ? Number(toDecimal(target)!.toString()) * 100 : null
	);
	const bandPct = $derived(band != null ? Number(toDecimal(band)?.toString() ?? 0) * 100 : null);
	const max = $derived(Math.max(cur, tgt ?? 0, 1) * 1.15);
	// 是否落在再平衡區間內
	const inBand = $derived(tgt != null && bandPct != null ? Math.abs(cur - tgt) <= bandPct : null);
</script>

<div class={cn('w-full', className)}>
	<div class="bg-muted relative h-2.5 w-full overflow-hidden rounded-full">
		<div
			class={cn('h-full rounded-full transition-all', inBand === false ? 'bg-up' : 'bg-primary')}
			style="width:{Math.min(100, (cur / max) * 100)}%"
		></div>
	</div>
	<div class="text-muted-foreground mt-1 flex items-center justify-between text-xs">
		<span class="tnum">目前 {cur.toFixed(1)}%</span>
		{#if tgt != null}
			<span class="tnum"
				>目標 {tgt.toFixed(1)}%{#if inBand === false}<span class="text-up">
						· 超出區間</span
					>{:else if inBand}<span class="text-down"> · 區間內</span>{/if}</span
			>
		{/if}
	</div>
</div>
