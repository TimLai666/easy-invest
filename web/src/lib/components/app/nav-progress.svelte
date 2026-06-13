<script lang="ts">
	import { navigating } from '$app/state';
</script>

{#if navigating.to}
	<div
		class="bg-primary fixed top-0 right-0 left-0 z-[100] h-0.5 origin-left animate-pulse"
		style="animation: nav-progress 2s ease-in-out infinite"
	></div>
{/if}

<style>
	@keyframes nav-progress {
		0% {
			transform: scaleX(0);
		}
		50% {
			transform: scaleX(0.7);
		}
		100% {
			transform: scaleX(1);
		}
	}
</style>
