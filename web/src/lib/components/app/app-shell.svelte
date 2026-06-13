<script lang="ts">
	import { page } from '$app/state';
	import { goto } from '$app/navigation';
	import type { Snippet } from 'svelte';
	import { navItems, navGroups } from '$lib/nav';
	import { session } from '$lib/stores/session.svelte';
	import { cn } from '$lib/utils';
	import FreshnessChip from './freshness-chip.svelte';
	import ThemeToggle from './theme-toggle.svelte';
	import { Button } from '$lib/components/ui/button';
	import {
		DropdownMenu,
		DropdownMenuTrigger,
		DropdownMenuContent,
		DropdownMenuItem,
		DropdownMenuLabel,
		DropdownMenuSeparator
	} from '$lib/components/ui/dropdown-menu';
	import MenuIcon from '@lucide/svelte/icons/menu';
	import LogOutIcon from '@lucide/svelte/icons/log-out';
	import TrendingUpIcon from '@lucide/svelte/icons/trending-up';
	import XIcon from '@lucide/svelte/icons/x';

	let { children }: { children: Snippet } = $props();

	let mobileOpen = $state(false);

	function isActive(href: string): boolean {
		if (href === '/') return page.url.pathname === '/';
		return page.url.pathname.startsWith(href);
	}

	// 取最長前綴相符的導覽項作為標題（子路由如 /recommendations/[id] 也能對應）
	const currentTitle = $derived(
		[...navItems].filter((n) => isActive(n.href)).sort((a, b) => b.href.length - a.href.length)[0]
			?.label ?? 'Easy Invest'
	);

	async function logout() {
		await session.logout();
		goto('/login');
	}

	const initials = $derived(
		(session.user?.display_name || session.user?.email || '?').slice(0, 1).toUpperCase()
	);
</script>

{#snippet sidebar()}
	<div class="flex h-full flex-col">
		<div class="flex h-16 items-center gap-2.5 px-5">
			<div
				class="bg-primary text-primary-foreground flex size-9 items-center justify-center rounded-lg shadow-sm"
			>
				<TrendingUpIcon class="size-5" />
			</div>
			<div class="leading-tight">
				<p class="font-semibold tracking-tight">Easy Invest</p>
				<p class="text-muted-foreground text-xs">台股投資建議</p>
			</div>
		</div>

		<nav class="scrollbar-thin flex-1 space-y-5 overflow-y-auto px-3 py-3">
			{#each navGroups as group (group)}
				<div>
					<p
						class="text-muted-foreground px-3 pb-1.5 text-[11px] font-medium tracking-wider uppercase"
					>
						{group}
					</p>
					<ul class="space-y-0.5">
						{#each navItems.filter((n) => n.group === group) as item (item.href)}
							{@const Icon = item.icon}
							<li>
								<a
									href={item.href}
									onclick={() => (mobileOpen = false)}
									class={cn(
										'flex items-center gap-3 rounded-lg px-3 py-2 text-sm font-medium transition-colors',
										isActive(item.href)
											? 'bg-sidebar-accent text-sidebar-accent-foreground'
											: 'text-muted-foreground hover:bg-sidebar-accent/60 hover:text-sidebar-accent-foreground'
									)}
								>
									<Icon class="size-4 shrink-0" />
									<span>{item.label}</span>
								</a>
							</li>
						{/each}
					</ul>
				</div>
			{/each}
		</nav>

		<div class="border-t p-3">
			<p class="text-muted-foreground px-2 text-[11px] leading-relaxed">
				僅提供投資建議，不代理下單。
			</p>
		</div>
	</div>
{/snippet}

<div class="bg-background flex h-dvh overflow-hidden">
	<!-- 桌面 sidebar -->
	<aside class="bg-sidebar hidden w-64 shrink-0 border-r lg:block">
		{@render sidebar()}
	</aside>

	<!-- 行動裝置 sidebar -->
	{#if mobileOpen}
		<div class="fixed inset-0 z-50 lg:hidden">
			<button
				class="absolute inset-0 bg-black/50"
				aria-label="關閉選單"
				onclick={() => (mobileOpen = false)}
			></button>
			<aside class="bg-sidebar absolute inset-y-0 left-0 w-64 border-r shadow-xl">
				<button
					class="hover:bg-accent absolute top-4 right-3 rounded-md p-1.5"
					aria-label="關閉"
					onclick={() => (mobileOpen = false)}><XIcon class="size-4" /></button
				>
				{@render sidebar()}
			</aside>
		</div>
	{/if}

	<div class="flex min-w-0 flex-1 flex-col">
		<header
			class="bg-background/80 sticky top-0 z-30 flex h-16 items-center gap-3 border-b px-4 backdrop-blur md:px-6"
		>
			<Button
				variant="ghost"
				size="icon"
				class="lg:hidden"
				onclick={() => (mobileOpen = true)}
				aria-label="開啟選單"><MenuIcon class="size-5" /></Button
			>
			<h1 class="text-base font-semibold md:text-lg">{currentTitle}</h1>

			<div class="ml-auto flex items-center gap-2">
				<FreshnessChip />
				<ThemeToggle />
				<DropdownMenu>
					<DropdownMenuTrigger>
						{#snippet child({ props })}
							<button
								{...props}
								class="bg-primary text-primary-foreground flex size-9 items-center justify-center rounded-full text-sm font-medium"
							>
								{initials}
							</button>
						{/snippet}
					</DropdownMenuTrigger>
					<DropdownMenuContent align="end" class="w-56">
						<DropdownMenuLabel>
							<div class="flex flex-col">
								<span class="truncate">{session.user?.display_name || '使用者'}</span>
								<span class="text-muted-foreground truncate text-xs font-normal"
									>{session.user?.email}</span
								>
							</div>
						</DropdownMenuLabel>
						<DropdownMenuSeparator />
						<DropdownMenuItem onclick={logout}>
							<LogOutIcon class="size-4" />登出
						</DropdownMenuItem>
					</DropdownMenuContent>
				</DropdownMenu>
			</div>
		</header>

		<main class="scrollbar-thin flex-1 overflow-y-auto">
			<div class="mx-auto w-full max-w-7xl p-4 md:p-6">
				{@render children()}
			</div>
		</main>
	</div>
</div>
