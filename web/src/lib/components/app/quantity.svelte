<script lang="ts">
	import { formatShares, sharesToLots } from '$lib/format';
	import { cn } from '$lib/utils';

	let {
		shares,
		lots = undefined,
		lotSize = 1000,
		inline = false,
		class: className = ''
	}: {
		shares: string | number | null | undefined;
		lots?: string | number | null;
		lotSize?: number;
		inline?: boolean;
		class?: string;
	} = $props();

	const lotsText = $derived(lots != null ? String(lots) : sharesToLots(shares, lotSize));
</script>

{#if inline}
	<span class={cn('tnum', className)}
		>{formatShares(shares)} 股<span class="text-muted-foreground"> · {lotsText} 張</span></span
	>
{:else}
	<span class={cn('tnum inline-flex flex-col leading-tight', className)}>
		<span>{formatShares(shares)} 股</span>
		<span class="text-muted-foreground text-xs">{lotsText} 張</span>
	</span>
{/if}
