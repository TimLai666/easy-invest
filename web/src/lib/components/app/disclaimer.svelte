<script lang="ts">
	import InfoIcon from '@lucide/svelte/icons/info';
	import { cn } from '$lib/utils';

	let {
		text,
		marketDataAsOf = null,
		portfolioAsOf = null,
		class: className = ''
	}: {
		text?: string;
		marketDataAsOf?: string | null;
		portfolioAsOf?: string | null;
		class?: string;
	} = $props();

	const DEFAULT_TEXT =
		'本系統提供之內容為依使用者輸入資料與歷史市場資料計算之參考資訊，不構成投資建議或要約，不保證獲利。投資人應自行判斷並承擔投資風險。';
</script>

<div
	class={cn(
		'text-muted-foreground bg-muted/50 flex gap-2.5 rounded-lg border border-dashed p-3 text-xs leading-relaxed',
		className
	)}
>
	<InfoIcon class="mt-0.5 size-4 shrink-0 opacity-70" />
	<div class="space-y-1">
		<p>{text || DEFAULT_TEXT}</p>
		{#if marketDataAsOf || portfolioAsOf}
			<p class="text-[11px] opacity-80">
				{#if marketDataAsOf}依據 {marketDataAsOf} 收盤資料計算{/if}{#if marketDataAsOf && portfolioAsOf}　·　{/if}{#if portfolioAsOf}庫存截至
					{portfolioAsOf}{/if}
			</p>
		{/if}
	</div>
</div>
