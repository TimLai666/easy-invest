<script lang="ts">
	import '../app.css';
	import '@fontsource-variable/noto-sans-tc';
	import '@fontsource-variable/jetbrains-mono';
	import { ModeWatcher } from 'mode-watcher';
	import { Toaster } from '$lib/components/ui/sonner';

	let { children } = $props();
</script>

<svelte:head>
	<title>Easy Invest · 台股投資建議</title>
	<meta name="description" content="以台股為主、API-first 的投資建議系統" />
</svelte:head>

<ModeWatcher />
<Toaster />
{@render children()}
