<script lang="ts">
	import { cn } from '$lib/utils';

	export interface DonutSlice {
		label: string;
		value: number;
		color: string;
	}

	let {
		data,
		size = 180,
		thickness = 22,
		class: className = '',
		centerLabel = '',
		centerValue = ''
	}: {
		data: DonutSlice[];
		size?: number;
		thickness?: number;
		class?: string;
		centerLabel?: string;
		centerValue?: string;
	} = $props();

	const total = $derived(data.reduce((s, d) => s + Math.max(0, d.value), 0));
	const radius = $derived((size - thickness) / 2);
	const circ = $derived(2 * Math.PI * radius);

	const segments = $derived.by(() => {
		let acc = 0;
		return data
			.filter((d) => d.value > 0)
			.map((d) => {
				const frac = total > 0 ? d.value / total : 0;
				const seg = { ...d, frac, offset: acc, dash: frac * circ };
				acc += frac;
				return seg;
			});
	});
</script>

<div class={cn('flex items-center gap-5', className)}>
	<div class="relative shrink-0" style="width:{size}px;height:{size}px">
		<svg width={size} height={size} viewBox="0 0 {size} {size}" class="-rotate-90">
			<circle
				cx={size / 2}
				cy={size / 2}
				r={radius}
				fill="none"
				stroke="var(--muted)"
				stroke-width={thickness}
			/>
			{#each segments as seg (seg.label)}
				<circle
					cx={size / 2}
					cy={size / 2}
					r={radius}
					fill="none"
					stroke={seg.color}
					stroke-width={thickness}
					stroke-dasharray="{seg.dash} {circ - seg.dash}"
					stroke-dashoffset={-seg.offset * circ}
					stroke-linecap="butt"
				/>
			{/each}
		</svg>
		{#if centerValue || centerLabel}
			<div class="absolute inset-0 flex flex-col items-center justify-center text-center">
				{#if centerValue}<span class="tnum text-lg font-semibold">{centerValue}</span>{/if}
				{#if centerLabel}<span class="text-muted-foreground text-xs">{centerLabel}</span>{/if}
			</div>
		{/if}
	</div>
	<ul class="flex-1 space-y-1.5 text-sm">
		{#each segments as seg (seg.label)}
			<li class="flex items-center justify-between gap-3">
				<span class="flex items-center gap-2">
					<span class="size-2.5 rounded-full" style="background:{seg.color}"></span>
					<span>{seg.label}</span>
				</span>
				<span class="tnum text-muted-foreground">{(seg.frac * 100).toFixed(1)}%</span>
			</li>
		{/each}
	</ul>
</div>
