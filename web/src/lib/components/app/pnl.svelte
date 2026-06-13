<script lang="ts">
	import { formatMoney, formatPercent, toneOf, toneClass } from '$lib/format';
	import { cn } from '$lib/utils';
	import TrendingUpIcon from '@lucide/svelte/icons/trending-up';
	import TrendingDownIcon from '@lucide/svelte/icons/trending-down';

	let {
		value,
		percent = null,
		dp = 0,
		showIcon = false,
		class: className = ''
	}: {
		value: string | number | null | undefined;
		percent?: string | number | null;
		dp?: number;
		showIcon?: boolean;
		class?: string;
	} = $props();

	const tone = $derived(toneOf(value));
</script>

<span class={cn('tnum inline-flex items-center gap-1 font-medium', toneClass(tone), className)}>
	{#if showIcon && tone === 'up'}<TrendingUpIcon class="size-3.5" />{/if}
	{#if showIcon && tone === 'down'}<TrendingDownIcon class="size-3.5" />{/if}
	<span>{formatMoney(value, { dp, sign: true })}</span>
	{#if percent !== null && percent !== undefined}
		<span class="opacity-80">({formatPercent(percent, 2, { sign: true })})</span>
	{/if}
</span>
