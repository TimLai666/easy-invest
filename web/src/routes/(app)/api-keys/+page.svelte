<script lang="ts">
	import { onMount } from 'svelte';
	import { apiKeys as apiKeysApi, ApiError } from '$lib/api';
	import type { ApiKey, ApiKeyWithSecret } from '$lib/api';
	import { formatDateTime } from '$lib/format';
	import { allScopes, scopeLabels } from '$lib/labels';

	import PageState from '$lib/components/app/page-state.svelte';
	import * as Card from '$lib/components/ui/card';
	import * as Table from '$lib/components/ui/table';
	import * as Dialog from '$lib/components/ui/dialog';
	import { Button } from '$lib/components/ui/button';
	import { Badge } from '$lib/components/ui/badge';
	import { Input } from '$lib/components/ui/input';
	import { Label } from '$lib/components/ui/label';
	import { Checkbox } from '$lib/components/ui/checkbox';
	import { toast } from '$lib/components/ui/sonner';

	import KeyRoundIcon from '@lucide/svelte/icons/key-round';
	import PlusIcon from '@lucide/svelte/icons/plus';
	import CopyIcon from '@lucide/svelte/icons/copy';
	import TriangleAlertIcon from '@lucide/svelte/icons/triangle-alert';
	import RotateCwIcon from '@lucide/svelte/icons/rotate-cw';
	import Trash2Icon from '@lucide/svelte/icons/trash-2';

	let items = $state<ApiKey[]>([]);
	let loading = $state(true);
	let error = $state<unknown>(null);

	async function load() {
		loading = true;
		error = null;
		try {
			items = await apiKeysApi.list();
		} catch (e) {
			error = e;
		} finally {
			loading = false;
		}
	}
	onMount(load);

	// 建立表單狀態
	let createOpen = $state(false);
	let name = $state('');
	let description = $state('');
	let selectedScopes = $state<Record<string, boolean>>({});
	let expiresAt = $state('');
	let creating = $state(false);

	function resetCreateForm() {
		name = '';
		description = '';
		selectedScopes = {};
		expiresAt = '';
	}

	function toggleScope(scope: string, value: boolean) {
		selectedScopes = { ...selectedScopes, [scope]: value };
	}

	const chosenScopes = $derived(allScopes.filter((s) => selectedScopes[s]));

	// 只顯示一次的金鑰（建立或輪替後）
	let secretOpen = $state(false);
	let secret = $state<ApiKeyWithSecret | null>(null);
	let secretTitle = $state('金鑰建立成功');

	function showSecret(key: ApiKeyWithSecret, title: string) {
		secret = key;
		secretTitle = title;
		secretOpen = true;
	}

	async function copySecret() {
		if (!secret) return;
		try {
			await navigator.clipboard.writeText(secret.plaintext);
			toast.success('已複製金鑰到剪貼簿');
		} catch {
			toast.error('複製失敗，請手動選取複製');
		}
	}

	function onSecretClose(open: boolean) {
		secretOpen = open;
		if (!open) {
			secret = null;
			load();
		}
	}

	async function submitCreate(e: SubmitEvent) {
		e.preventDefault();
		if (!name.trim()) {
			toast.error('請輸入金鑰名稱');
			return;
		}
		if (chosenScopes.length === 0) {
			toast.error('請至少選擇一個權限範圍');
			return;
		}
		creating = true;
		try {
			const created = await apiKeysApi.create({
				name: name.trim(),
				description: description.trim() || undefined,
				scopes: chosenScopes,
				expires_at: expiresAt ? expiresAt : null
			});
			createOpen = false;
			resetCreateForm();
			toast.success('金鑰已建立');
			showSecret(created, '金鑰建立成功');
		} catch (err) {
			toast.error(err instanceof ApiError ? err.message : '建立金鑰失敗');
		} finally {
			creating = false;
		}
	}

	// 輪替
	let rotating = $state<string | null>(null);
	async function doRotate(key: ApiKey) {
		rotating = key.id;
		try {
			const rotated = await apiKeysApi.rotate(key.id);
			toast.success('金鑰已輪替，舊金鑰已失效');
			showSecret(rotated, '金鑰輪替成功');
		} catch (err) {
			toast.error(err instanceof ApiError ? err.message : '輪替金鑰失敗');
		} finally {
			rotating = null;
		}
	}

	// 撤銷
	let revokeOpen = $state(false);
	let revokeTarget = $state<ApiKey | null>(null);
	let revoking = $state(false);

	function askRevoke(key: ApiKey) {
		revokeTarget = key;
		revokeOpen = true;
	}

	async function confirmRevoke() {
		if (!revokeTarget) return;
		revoking = true;
		try {
			await apiKeysApi.revoke(revokeTarget.id);
			toast.success('金鑰已撤銷');
			revokeOpen = false;
			revokeTarget = null;
			await load();
		} catch (err) {
			toast.error(err instanceof ApiError ? err.message : '撤銷金鑰失敗');
		} finally {
			revoking = false;
		}
	}

	function isRevoked(key: ApiKey): boolean {
		return !!key.revoked_at;
	}
</script>

<div class="space-y-6">
	<div class="flex flex-col gap-3 sm:flex-row sm:items-end sm:justify-between">
		<div>
			<h1 class="text-2xl font-semibold tracking-tight">API 金鑰</h1>
			<p class="text-muted-foreground mt-1 max-w-2xl text-sm">
				API
				金鑰僅儲存雜湊，明文只在建立或輪替時顯示一次，可隨時撤銷或輪替。請以金鑰透過程式存取你自己的資料。
			</p>
		</div>
		<Button
			onclick={() => {
				resetCreateForm();
				createOpen = true;
			}}
		>
			<PlusIcon class="size-4" />建立金鑰
		</Button>
	</div>

	<PageState
		{loading}
		{error}
		empty={items.length === 0}
		onRetry={load}
		emptyIcon={KeyRoundIcon}
		emptyTitle="還沒有任何 API 金鑰"
		emptyHint="建立一把金鑰後，你就能以程式（而非登入畫面）存取自己的庫存、交易與建議資料。明文只會顯示一次，請妥善保存。"
	>
		{#snippet emptyAction()}
			<Button
				onclick={() => {
					resetCreateForm();
					createOpen = true;
				}}
			>
				<PlusIcon class="size-4" />建立第一把金鑰
			</Button>
		{/snippet}

		<Card.Root>
			<Card.Header>
				<Card.Title>金鑰列表</Card.Title>
				<Card.Description>共 {items.length} 把金鑰</Card.Description>
			</Card.Header>
			<Card.Content class="px-0">
				<div class="scrollbar-thin overflow-x-auto">
					<Table.Root>
						<Table.Header>
							<Table.Row>
								<Table.Head>名稱</Table.Head>
								<Table.Head>前綴</Table.Head>
								<Table.Head>權限範圍</Table.Head>
								<Table.Head>建立時間</Table.Head>
								<Table.Head>最後使用</Table.Head>
								<Table.Head>到期</Table.Head>
								<Table.Head>狀態</Table.Head>
								<Table.Head class="text-right">操作</Table.Head>
							</Table.Row>
						</Table.Header>
						<Table.Body>
							{#each items as key (key.id)}
								<Table.Row class={isRevoked(key) ? 'opacity-50' : 'hover:bg-muted/40'}>
									<Table.Cell>
										<div class="flex flex-col">
											<span class="font-medium">{key.name}</span>
											{#if key.description}
												<span class="text-muted-foreground text-xs">{key.description}</span>
											{/if}
										</div>
									</Table.Cell>
									<Table.Cell>
										<code class="bg-muted rounded px-1.5 py-0.5 font-mono text-xs"
											>{key.key_prefix}</code
										>
									</Table.Cell>
									<Table.Cell>
										<div class="flex max-w-[16rem] flex-wrap gap-1">
											{#each key.scopes.slice(0, 3) as s (s)}
												<Badge variant="secondary" class="font-normal"
													>{scopeLabels[s as keyof typeof scopeLabels] ?? s}</Badge
												>
											{/each}
											{#if key.scopes.length > 3}
												<Badge variant="outline" class="font-normal">+{key.scopes.length - 3}</Badge
												>
											{/if}
										</div>
									</Table.Cell>
									<Table.Cell class="text-muted-foreground text-sm"
										>{formatDateTime(key.created_at)}</Table.Cell
									>
									<Table.Cell class="text-muted-foreground text-sm">
										{key.last_used_at ? formatDateTime(key.last_used_at) : '未使用'}
									</Table.Cell>
									<Table.Cell class="text-muted-foreground text-sm">
										{key.expires_at ? formatDateTime(key.expires_at) : '無'}
									</Table.Cell>
									<Table.Cell>
										{#if isRevoked(key)}
											<Badge variant="outline" class="text-muted-foreground font-normal"
												>已撤銷</Badge
											>
										{:else}
											<Badge variant="secondary" class="font-normal">使用中</Badge>
										{/if}
									</Table.Cell>
									<Table.Cell class="text-right">
										{#if !isRevoked(key)}
											<div class="flex justify-end gap-1">
												<Button
													variant="ghost"
													size="sm"
													disabled={rotating === key.id}
													onclick={() => doRotate(key)}
												>
													<RotateCwIcon class="size-4" />輪替
												</Button>
												<Button
													variant="ghost"
													size="sm"
													class="text-destructive hover:text-destructive"
													onclick={() => askRevoke(key)}
												>
													<Trash2Icon class="size-4" />撤銷
												</Button>
											</div>
										{:else}
											<span class="text-muted-foreground text-xs">—</span>
										{/if}
									</Table.Cell>
								</Table.Row>
							{/each}
						</Table.Body>
					</Table.Root>
				</div>
			</Card.Content>
		</Card.Root>
	</PageState>
</div>

<!-- 建立金鑰 Dialog -->
<Dialog.Root bind:open={createOpen}>
	<Dialog.Content class="sm:max-w-lg">
		<Dialog.Header>
			<Dialog.Title>建立 API 金鑰</Dialog.Title>
			<Dialog.Description
				>選擇此金鑰可存取的權限範圍。明文金鑰只會在建立後顯示一次。</Dialog.Description
			>
		</Dialog.Header>
		<form onsubmit={submitCreate} class="space-y-4">
			<div class="space-y-2">
				<Label for="key-name">名稱</Label>
				<Input id="key-name" bind:value={name} placeholder="例如：行動 App、回測腳本" />
			</div>
			<div class="space-y-2">
				<Label for="key-desc">用途說明（選填）</Label>
				<Input id="key-desc" bind:value={description} placeholder="這把金鑰用來做什麼" />
			</div>
			<div class="space-y-2">
				<Label>權限範圍</Label>
				<div class="grid gap-2 rounded-lg border p-3 sm:grid-cols-2">
					{#each allScopes as scope (scope)}
						<label class="flex items-center gap-2 text-sm">
							<Checkbox
								checked={!!selectedScopes[scope]}
								onCheckedChange={(v) => toggleScope(scope, v === true)}
							/>
							<span>{scopeLabels[scope]}</span>
						</label>
					{/each}
				</div>
				<p class="text-muted-foreground text-xs">已選擇 {chosenScopes.length} 項</p>
			</div>
			<div class="space-y-2">
				<Label for="key-exp">到期日（選填）</Label>
				<Input id="key-exp" type="date" bind:value={expiresAt} />
			</div>
			<Dialog.Footer>
				<Button type="button" variant="outline" onclick={() => (createOpen = false)}>取消</Button>
				<Button type="submit" disabled={creating}>{creating ? '建立中…' : '建立金鑰'}</Button>
			</Dialog.Footer>
		</form>
	</Dialog.Content>
</Dialog.Root>

<!-- 只顯示一次的金鑰 Dialog -->
<Dialog.Root bind:open={secretOpen} onOpenChange={onSecretClose}>
	<Dialog.Content class="sm:max-w-lg">
		<Dialog.Header>
			<Dialog.Title>{secretTitle}</Dialog.Title>
			<Dialog.Description
				>請立即複製並妥善保存。關閉這個視窗後，將無法再取得明文金鑰。</Dialog.Description
			>
		</Dialog.Header>
		{#if secret}
			<div class="space-y-3">
				<div
					class="border-destructive/30 bg-destructive/5 text-destructive flex items-start gap-2 rounded-lg border p-3 text-sm"
				>
					<TriangleAlertIcon class="mt-0.5 size-4 shrink-0" />
					<span
						>明文金鑰僅顯示這一次，系統只保存雜湊值，無法再次還原。關閉後若遺失只能輪替或重新建立。</span
					>
				</div>
				<div class="flex items-center gap-2">
					<code
						class="bg-muted scrollbar-thin block w-full overflow-x-auto rounded-lg p-3 font-mono text-sm break-all"
					>
						{secret.plaintext}
					</code>
				</div>
				<Button variant="outline" onclick={copySecret} class="w-full">
					<CopyIcon class="size-4" />複製金鑰
				</Button>
			</div>
		{/if}
		<Dialog.Footer>
			<Button onclick={() => onSecretClose(false)}>我已保存，關閉</Button>
		</Dialog.Footer>
	</Dialog.Content>
</Dialog.Root>

<!-- 撤銷確認 Dialog -->
<Dialog.Root bind:open={revokeOpen}>
	<Dialog.Content class="sm:max-w-md">
		<Dialog.Header>
			<Dialog.Title>撤銷金鑰</Dialog.Title>
			<Dialog.Description>
				{#if revokeTarget}
					確定要撤銷「{revokeTarget.name}」（{revokeTarget.key_prefix}）嗎？撤銷後使用此金鑰的程式將立即無法存取，且無法復原。
				{/if}
			</Dialog.Description>
		</Dialog.Header>
		<Dialog.Footer>
			<Button type="button" variant="outline" onclick={() => (revokeOpen = false)}>取消</Button>
			<Button type="button" variant="destructive" disabled={revoking} onclick={confirmRevoke}>
				{revoking ? '撤銷中…' : '確認撤銷'}
			</Button>
		</Dialog.Footer>
	</Dialog.Content>
</Dialog.Root>
