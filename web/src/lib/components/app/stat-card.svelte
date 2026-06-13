<script lang="ts">
	import type { Snippet } from 'svelte';
	import { cn } from '$lib/utils';
	import type { Component } from 'svelte';

	let {
		label,
		icon = undefined,
		hint = undefined,
		accent = false,
		value,
		footer
	}: {
		label: string;
		icon?: Component;
		hint?: string;
		accent?: boolean;
		value: Snippet;
		footer?: Snippet;
	} = $props();
	const Icon = $derived(icon);
</script>

<div
	class={cn(
		'bg-card flex flex-col gap-2 rounded-xl border p-4 shadow-xs transition-shadow hover:shadow-sm',
		accent && 'ring-primary/20 ring-1'
	)}
>
	<div class="text-muted-foreground flex items-center justify-between text-sm">
		<span>{label}</span>
		{#if Icon}<Icon class="size-4 opacity-60" />{/if}
	</div>
	<div class="text-2xl font-semibold tracking-tight">
		{@render value()}
	</div>
	{#if footer}
		<div class="text-muted-foreground text-xs">{@render footer()}</div>
	{:else if hint}
		<div class="text-muted-foreground text-xs">{hint}</div>
	{/if}
</div>
