<script lang="ts">
	import { cn } from '$lib/utils';
	import { formatMoney, formatDate } from '$lib/format';

	export interface Series {
		label: string;
		color: string;
		points: { x: string; y: number }[];
	}

	let {
		series,
		height = 260,
		class: className = '',
		yPrefix = ''
	}: {
		series: Series[];
		height?: number;
		class?: string;
		yPrefix?: string;
	} = $props();

	const W = 760;
	const padL = 56;
	const padR = 16;
	const padT = 16;
	const padB = 28;

	const allY = $derived(series.flatMap((s) => s.points.map((p) => p.y)));
	const allX = $derived(series[0]?.points.map((p) => p.x) ?? []);
	const minY = $derived(allY.length ? Math.min(...allY) : 0);
	const maxY = $derived(allY.length ? Math.max(...allY) : 1);
	const range = $derived(maxY - minY || 1);
	const n = $derived(allX.length);

	function sx(i: number): number {
		if (n <= 1) return padL;
		return padL + (i / (n - 1)) * (W - padL - padR);
	}
	function sy(y: number): number {
		return padT + (1 - (y - minY) / range) * (height - padT - padB);
	}

	function path(points: { x: string; y: number }[]): string {
		return points
			.map((p, i) => `${i === 0 ? 'M' : 'L'} ${sx(i).toFixed(1)} ${sy(p.y).toFixed(1)}`)
			.join(' ');
	}

	const ticks = $derived([minY, minY + range / 2, maxY]);
	const xLabels = $derived(n <= 1 ? allX : [allX[0], allX[Math.floor(n / 2)], allX[n - 1]]);
</script>

<div class={cn('w-full', className)}>
	<svg viewBox="0 0 {W} {height}" class="w-full" role="img" aria-label="走勢圖">
		{#each ticks as t (t)}
			<line x1={padL} x2={W - padR} y1={sy(t)} y2={sy(t)} stroke="var(--border)" stroke-width="1" />
			<text x={padL - 8} y={sy(t) + 4} text-anchor="end" class="fill-muted-foreground text-[11px]"
				>{yPrefix}{formatMoney(t, { dp: 0 })}</text
			>
		{/each}
		{#each xLabels as lbl, i (lbl + i)}
			{@const xi = n <= 1 ? padL : padL + (i / (xLabels.length - 1)) * (W - padL - padR)}
			<text x={xi} y={height - 8} text-anchor="middle" class="fill-muted-foreground text-[11px]"
				>{formatDate(lbl)}</text
			>
		{/each}
		{#each series as s (s.label)}
			{#if s.points.length}
				<path
					d={path(s.points)}
					fill="none"
					stroke={s.color}
					stroke-width="2"
					stroke-linejoin="round"
				/>
			{/if}
		{/each}
	</svg>
	{#if series.length > 1}
		<ul class="mt-2 flex flex-wrap gap-4 text-xs">
			{#each series as s (s.label)}
				<li class="flex items-center gap-1.5">
					<span class="h-0.5 w-4 rounded" style="background:{s.color}"></span>{s.label}
				</li>
			{/each}
		</ul>
	{/if}
</div>
