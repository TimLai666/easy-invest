<script lang="ts">
	import { toggleMode, mode } from 'mode-watcher';
	import SunIcon from '@lucide/svelte/icons/sun';
	import MoonIcon from '@lucide/svelte/icons/moon';
	import { Button } from '$lib/components/ui/button';
</script>

<Button variant="ghost" size="icon" onclick={toggleMode} aria-label="切換深淺色主題">
	{#if mode.current === 'dark'}
		<MoonIcon class="size-4" />
	{:else}
		<SunIcon class="size-4" />
	{/if}
</Button>
