<script lang="ts">
	import { page } from '$app/state';
	import { Button } from '$lib/components/ui/button';
	import AlertTriangleIcon from '@lucide/svelte/icons/triangle-alert';
	import SearchXIcon from '@lucide/svelte/icons/search-x';
	import HomeIcon from '@lucide/svelte/icons/home';
</script>

<svelte:head>
	<title>{page.status === 404 ? '找不到頁面' : '發生錯誤'} · Easy Invest</title>
</svelte:head>

<div class="bg-muted/30 flex min-h-dvh flex-col items-center justify-center p-6 text-center">
	<div
		class={page.status === 404
			? 'bg-muted text-muted-foreground flex size-16 items-center justify-center rounded-full'
			: 'bg-destructive/10 text-destructive flex size-16 items-center justify-center rounded-full'}
	>
		{#if page.status === 404}
			<SearchXIcon class="size-8" />
		{:else}
			<AlertTriangleIcon class="size-8" />
		{/if}
	</div>
	<h1 class="mt-6 text-3xl font-semibold tracking-tight">{page.status}</h1>
	<p class="text-muted-foreground mt-2 max-w-md text-sm">
		{#if page.status === 404}
			找不到這個頁面。網址可能已變更或頁面已移除。
		{:else}
			發生了未預期的錯誤，請稍後再試。如果問題持續發生，請確認後端是否正常運作。
		{/if}
	</p>
	{#if page.error?.message}
		<p class="text-muted-foreground mt-1 text-xs opacity-70">{page.error.message}</p>
	{/if}
	<div class="mt-6 flex gap-3">
		<Button href="/" variant="default"><HomeIcon class="size-4" />回到儀表板</Button>
		{#if page.status !== 404}
			<Button variant="outline" onclick={() => location.reload()}>重新載入</Button>
		{/if}
	</div>
</div>
