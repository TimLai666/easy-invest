<script lang="ts">
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import { session } from '$lib/stores/session.svelte';
	import { ApiError } from '$lib/api';
	import { Button } from '$lib/components/ui/button';
	import { Input } from '$lib/components/ui/input';
	import { Label } from '$lib/components/ui/label';
	import * as Tabs from '$lib/components/ui/tabs';
	import { toast } from '$lib/components/ui/sonner';
	import TrendingUpIcon from '@lucide/svelte/icons/trending-up';
	import ShieldCheckIcon from '@lucide/svelte/icons/shield-check';
	import LockIcon from '@lucide/svelte/icons/lock';
	import EyeOffIcon from '@lucide/svelte/icons/eye-off';

	let tab = $state<'login' | 'register'>('login');
	let email = $state('');
	let password = $state('');
	let displayName = $state('');
	let busy = $state(false);
	let formError = $state('');
	let registrationClosed = $state(false);

	const redirect = $derived(page.url.searchParams.get('redirect') || '/');

	onMount(async () => {
		// 已登入就直接進入
		if (!session.user) await session.refresh();
		if (session.user) goto(redirect);
	});

	async function submitLogin(e: SubmitEvent) {
		e.preventDefault();
		formError = '';
		busy = true;
		try {
			await session.login(email.trim(), password);
			toast.success('登入成功');
			goto(redirect);
		} catch (err) {
			formError = err instanceof ApiError ? err.message : '登入失敗，請稍後再試。';
		} finally {
			busy = false;
		}
	}

	async function submitRegister(e: SubmitEvent) {
		e.preventDefault();
		formError = '';
		if (password.length < 10) {
			formError = '密碼至少需 10 個字元。';
			return;
		}
		busy = true;
		try {
			await session.register(email.trim(), password, displayName.trim());
			toast.success('註冊成功，請登入');
			tab = 'login';
		} catch (err) {
			if (err instanceof ApiError && err.code === 'forbidden') {
				registrationClosed = true;
				formError = '此系統目前未開放註冊，請使用既有帳號登入。';
			} else {
				formError = err instanceof ApiError ? err.message : '註冊失敗，請稍後再試。';
			}
		} finally {
			busy = false;
		}
	}
</script>

<svelte:head><title>登入 · Easy Invest</title></svelte:head>

<div class="bg-muted/30 flex min-h-dvh items-center justify-center p-4">
	<div class="grid w-full max-w-4xl overflow-hidden rounded-2xl border shadow-lg md:grid-cols-2">
		<!-- 品牌側 -->
		<div class="bg-primary text-primary-foreground hidden flex-col justify-between p-8 md:flex">
			<div class="flex items-center gap-2.5">
				<div class="bg-primary-foreground/15 flex size-9 items-center justify-center rounded-lg">
					<TrendingUpIcon class="size-5" />
				</div>
				<span class="text-lg font-semibold">Easy Invest</span>
			</div>
			<div class="space-y-4">
				<h2 class="text-2xl font-semibold leading-snug">可追蹤、可解釋的<br />台股投資建議</h2>
				<ul class="space-y-2.5 text-sm opacity-90">
					<li class="flex items-center gap-2">
						<ShieldCheckIcon class="size-4" />庫存由交易流水計算，不只是 CRUD
					</li>
					<li class="flex items-center gap-2">
						<LockIcon class="size-4" />API key 僅存雜湊，可撤銷輪替
					</li>
					<li class="flex items-center gap-2">
						<EyeOffIcon class="size-4" />只提供建議，不代理下單
					</li>
				</ul>
			</div>
			<p class="text-xs opacity-70">本系統內容僅供參考，不構成投資建議，不保證獲利。</p>
		</div>

		<!-- 表單側 -->
		<div class="bg-card p-8">
			<div class="mb-6 flex items-center gap-2.5 md:hidden">
				<div
					class="bg-primary text-primary-foreground flex size-9 items-center justify-center rounded-lg"
				>
					<TrendingUpIcon class="size-5" />
				</div>
				<span class="text-lg font-semibold">Easy Invest</span>
			</div>

			<Tabs.Root bind:value={tab}>
				<Tabs.List class="grid w-full grid-cols-2">
					<Tabs.Trigger value="login">登入</Tabs.Trigger>
					<Tabs.Trigger value="register">註冊</Tabs.Trigger>
				</Tabs.List>

				<Tabs.Content value="login" class="mt-6">
					<form class="space-y-4" onsubmit={submitLogin}>
						<div class="space-y-2">
							<Label for="login-email">電子郵件</Label>
							<Input
								id="login-email"
								type="email"
								bind:value={email}
								required
								autocomplete="email"
								placeholder="you@example.com"
							/>
						</div>
						<div class="space-y-2">
							<Label for="login-password">密碼</Label>
							<Input
								id="login-password"
								type="password"
								bind:value={password}
								required
								autocomplete="current-password"
							/>
						</div>
						{#if formError}<p class="text-destructive text-sm">{formError}</p>{/if}
						<Button type="submit" class="w-full" disabled={busy}>{busy ? '登入中…' : '登入'}</Button
						>
					</form>
				</Tabs.Content>

				<Tabs.Content value="register" class="mt-6">
					<form class="space-y-4" onsubmit={submitRegister}>
						<div class="space-y-2">
							<Label for="reg-name">顯示名稱</Label>
							<Input id="reg-name" bind:value={displayName} placeholder="選填" />
						</div>
						<div class="space-y-2">
							<Label for="reg-email">電子郵件</Label>
							<Input id="reg-email" type="email" bind:value={email} required autocomplete="email" />
						</div>
						<div class="space-y-2">
							<Label for="reg-password">密碼</Label>
							<Input
								id="reg-password"
								type="password"
								bind:value={password}
								required
								autocomplete="new-password"
							/>
							<p class="text-muted-foreground text-xs">至少 10 個字元。</p>
						</div>
						{#if formError}<p class="text-destructive text-sm">{formError}</p>{/if}
						<Button type="submit" class="w-full" disabled={busy || registrationClosed}>
							{busy ? '處理中…' : '建立帳號'}
						</Button>
					</form>
				</Tabs.Content>
			</Tabs.Root>
		</div>
	</div>
</div>
