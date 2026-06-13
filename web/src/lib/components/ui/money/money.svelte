<script lang="ts">
	import { formatMoney } from '$lib/format';
	import { cn } from '$lib/utils';

	let {
		value,
		dp = 0,
		sign = false,
		currency = false,
		class: className = ''
	}: {
		value: string | number | null | undefined;
		dp?: number;
		sign?: boolean;
		currency?: boolean;
		class?: string;
	} = $props();
</script>

<span class={cn('tnum', className)}
	>{formatMoney(value, { dp, sign })}{#if currency}<span class="text-muted-foreground ml-1 text-xs"
			>TWD</span
		>{/if}</span
>
