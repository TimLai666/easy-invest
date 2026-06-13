<script lang="ts">
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import { session } from '$lib/stores/session.svelte';
	import AppShell from '$lib/components/app/app-shell.svelte';
	import TrendingUpIcon from '@lucide/svelte/icons/trending-up';

	let { children } = $props();

	onMount(async () => {
		if (!session.user) await session.refresh();
		if (!session.user) {
			const redirect = encodeURIComponent(page.url.pathname + page.url.search);
			goto(`/login?redirect=${redirect}`);
		}
	});
</script>

{#if session.loading && !session.user}
	<div class="bg-background flex h-dvh flex-col items-center justify-center gap-4">
		<div
			class="bg-primary text-primary-foreground flex size-12 animate-pulse items-center justify-center rounded-xl"
		>
			<TrendingUpIcon class="size-6" />
		</div>
		<p class="text-muted-foreground text-sm">載入中…</p>
	</div>
{:else if session.user}
	<AppShell>
		{@render children()}
	</AppShell>
{/if}
