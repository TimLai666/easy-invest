<script lang="ts">
	import type { Snippet } from 'svelte';
	import { Skeleton } from '$lib/components/ui/skeleton';
	import { Button } from '$lib/components/ui/button';
	import { ApiError } from '$lib/api';
	import AlertTriangleIcon from '@lucide/svelte/icons/triangle-alert';
	import RefreshCwIcon from '@lucide/svelte/icons/refresh-cw';
	import InboxIcon from '@lucide/svelte/icons/inbox';
	import type { Component } from 'svelte';

	let {
		loading = false,
		error = null,
		empty = false,
		onRetry = undefined,
		emptyTitle = '目前沒有資料',
		emptyHint = '',
		emptyIcon = undefined,
		skeleton,
		emptyAction,
		children
	}: {
		loading?: boolean;
		error?: unknown;
		empty?: boolean;
		onRetry?: () => void;
		emptyTitle?: string;
		emptyHint?: string;
		emptyIcon?: Component;
		skeleton?: Snippet;
		emptyAction?: Snippet;
		children: Snippet;
	} = $props();

	const EmptyIcon = $derived(emptyIcon);
	const errMsg = $derived(
		error instanceof ApiError ? error.message : error instanceof Error ? error.message : ''
	);
</script>

{#if loading}
	{#if skeleton}
		{@render skeleton()}
	{:else}
		<div class="space-y-3">
			<Skeleton class="h-24 w-full rounded-xl" />
			<Skeleton class="h-64 w-full rounded-xl" />
		</div>
	{/if}
{:else if error}
	<div
		class="border-destructive/30 bg-destructive/5 flex flex-col items-center gap-3 rounded-xl border p-10 text-center"
	>
		<div
			class="bg-destructive/10 text-destructive flex size-12 items-center justify-center rounded-full"
		>
			<AlertTriangleIcon class="size-6" />
		</div>
		<div>
			<p class="font-medium">讀取失敗</p>
			<p class="text-muted-foreground mt-1 max-w-md text-sm">
				{errMsg || '發生未預期的錯誤，請稍後再試。'}
			</p>
		</div>
		{#if onRetry}
			<Button variant="outline" size="sm" onclick={onRetry}>
				<RefreshCwIcon class="size-4" />重試
			</Button>
		{/if}
	</div>
{:else if empty}
	<div
		class="bg-muted/30 flex flex-col items-center gap-3 rounded-xl border border-dashed p-12 text-center"
	>
		<div
			class="bg-muted text-muted-foreground flex size-12 items-center justify-center rounded-full"
		>
			{#if EmptyIcon}<EmptyIcon class="size-6" />{:else}<InboxIcon class="size-6" />{/if}
		</div>
		<div>
			<p class="font-medium">{emptyTitle}</p>
			{#if emptyHint}<p class="text-muted-foreground mt-1 max-w-md text-sm">{emptyHint}</p>{/if}
		</div>
		{#if emptyAction}{@render emptyAction()}{/if}
	</div>
{:else}
	{@render children()}
{/if}
