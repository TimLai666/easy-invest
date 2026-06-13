<script lang="ts">
	import { onMount } from 'svelte';

	type Screen =
		| 'auth'
		| 'dashboard'
		| 'lots'
		| 'ledger'
		| 'recommendations'
		| 'reconciliation'
		| 'backtests'
		| 'settings'
		| 'apiKeys'
		| 'market';

	type NavItem = {
		id: Screen;
		label: string;
		icon: string;
		endpoint?: string;
		emptyTitle: string;
		emptyAction: string;
	};

	const navItems: NavItem[] = [
		{ id: 'auth', label: '登入', icon: 'M12 3a4 4 0 0 1 4 4v2h1a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2v-8a2 2 0 0 1 2-2h1V7a4 4 0 0 1 4-4Zm-2 6h4V7a2 2 0 0 0-4 0v2Z', emptyTitle: '尚未登入', emptyAction: '建立 session' },
		{ id: 'dashboard', label: '庫存', icon: 'M4 19V5h3v14H4Zm6 0V9h3v10h-3Zm6 0V3h3v16h-3Z', endpoint: '/portfolio', emptyTitle: '還沒有庫存', emptyAction: '新增第一筆交易' },
		{ id: 'lots', label: '批次', icon: 'M4 5h16v4H4V5Zm0 6h16v4H4v-4Zm0 6h16v2H4v-2Z', endpoint: '/portfolio/lots', emptyTitle: '沒有批次明細', emptyAction: '新增買進事件' },
		{ id: 'ledger', label: '事件', icon: 'M6 3h12v18H6V3Zm3 4h6v2H9V7Zm0 4h6v2H9v-2Zm0 4h4v2H9v-2Z', endpoint: '/ledger/events', emptyTitle: '還沒有事件', emptyAction: '建立交易事件' },
		{ id: 'recommendations', label: '建議', icon: 'M4 12h5l2-7 3 14 2-7h4', endpoint: '/recommendations/runs', emptyTitle: '尚無建議紀錄', emptyAction: '產生建議' },
		{ id: 'reconciliation', label: '對帳', icon: 'M5 5h14v4H5V5Zm0 6h8v8H5v-8Zm10 0h4v8h-4v-8Z', endpoint: '/reconciliation/broker-snapshots', emptyTitle: '尚未匯入券商資料', emptyAction: '上傳對帳檔' },
		{ id: 'backtests', label: '回測', icon: 'M4 18 9 9l4 5 6-10 1 3-7 12-4-5-4 7Z', endpoint: '/backtests/runs', emptyTitle: '還沒有回測', emptyAction: '建立回測' },
		{ id: 'settings', label: '設定', icon: 'M12 8a4 4 0 1 1 0 8 4 4 0 0 1 0-8Zm0-5 1 3 3 1 3-1 2 3-2 3 1 3-3 1-1 3h-4l-1-3-3-1-3 1-2-3 2-3-1-3 3-1 1-3h4Z', endpoint: '/settings', emptyTitle: '尚未建立設定', emptyAction: '使用預設值' },
		{ id: 'apiKeys', label: 'API key', icon: 'M8 14a4 4 0 1 1 3.5-2.1L20 3v4h-3v3h-3v3h-3l-1.2 1.2A4 4 0 0 1 8 14Zm0 2a2 2 0 1 0 0-4 2 2 0 0 0 0 4Z', endpoint: '/api-keys', emptyTitle: '沒有 API key', emptyAction: '建立 API key' },
		{ id: 'market', label: '市場資料', icon: 'M4 17h16v2H4v-2Zm2-4h3v3H6v-3Zm5-5h3v8h-3V8Zm5-3h3v11h-3V5Z', endpoint: '/market/freshness', emptyTitle: '沒有市場資料', emptyAction: '重新整理' }
	];

	const holdings = [
		{ symbol: '0050', name: '元大台灣50', shares: '2,000', lots: '2', originalCost: '365,420', adjustedCost: '356,210', marketValue: '384,000', pnl: '+27,790', pnlTone: 'gain', weight: 36, target: 40 },
		{ symbol: '2330', name: '台積電', shares: '600', lots: '0.6', originalCost: '602,130', adjustedCost: '596,130', marketValue: '642,000', pnl: '+45,870', pnlTone: 'gain', weight: 60, target: 55 },
		{ symbol: '00878', name: '國泰永續高股息', shares: '300', lots: '0.3', originalCost: '6,240', adjustedCost: '6,020', marketValue: '6,450', pnl: '+430', pnlTone: 'gain', weight: 4, target: 5 }
	];

	const lots = [
		{ symbol: '2330', opened: '2026-01-10', shares: '400', original: '400,342', adjusted: '397,942', remaining: '400' },
		{ symbol: '2330', opened: '2026-04-18', shares: '200', original: '201,788', adjusted: '198,188', remaining: '200' },
		{ symbol: '0050', opened: '2026-02-15', shares: '2,000', original: '365,420', adjusted: '356,210', remaining: '2,000' }
	];

	const events = [
		{ id: 'E-2401', date: '2026-06-10', type: '買進', symbol: '0050', quantity: '1 張 / 1,000 股', amount: '-182,720', source: 'manual' },
		{ id: 'E-2392', date: '2026-05-21', type: '現金股利', symbol: '2330', quantity: '600 股', amount: '+7,074', source: 'corporate_action' },
		{ id: 'E-2358', date: '2026-04-18', type: '買進', symbol: '2330', quantity: '200 股', amount: '-201,788', source: 'manual' }
	];

	const recommendations = [
		{ action: '買進', symbol: '0050', quantity: '1,000 股 / 1 張', amount: '192,000', feeTax: '273', current: 36, target: 40, reason: '0050 低於目標權重且超出再平衡區間，買進後可讓配置接近目標。', risk: '依 2026-06-12 收盤價估算，實際成交價與費稅可能不同。', confidence: '0.72', status: 'draft' },
		{ action: '賣出', symbol: '2330', quantity: '100 股 / 0.1 張', amount: '107,000', feeTax: '474', current: 60, target: 55, reason: '2330 高於目標權重，先賣出部分部位再配置到低權重標的。', risk: '賣出會實現損益，請自行確認稅費與券商成交回報。', confidence: '0.68', status: 'draft' }
	];

	const diffs = [
		{ type: 'avg_cost_mismatch', symbol: '2330', internal: '993.55', broker: '986.88', status: 'pending' },
		{ type: 'missing_dividend', symbol: '0050', internal: '0', broker: '3,600', status: 'pending' },
		{ type: 'quantity_mismatch', symbol: '00878', internal: '300', broker: '0', status: 'fixed_input' }
	];

	const freshness = [
		{ dataset: '上市日終行情', source: 'TWSE', asOf: '2026-06-12', lag: 1, status: 'healthy' },
		{ dataset: '上櫃日終行情', source: 'TPEx', asOf: '2026-06-12', lag: 1, status: 'healthy' },
		{ dataset: '公司行動', source: 'TWSE/TPEx', asOf: '2026-06-11', lag: 2, status: 'degraded' },
		{ dataset: '交易日曆', source: 'TWSE', asOf: '2026-12-31', lag: 0, status: 'healthy' }
	];

	const backtestCurve = [100, 104, 98, 111, 116, 123, 119, 128];

	let active = $state<Screen>('dashboard');
	let collapsed = $state(false);
	let dark = $state(false);
	let apiBase = $state('/api/v1');
	let loading = $state<Record<string, boolean>>({});
	let errors = $state<Record<string, string>>({});
	let latestPayload = $state<Record<string, unknown>>({});
	let marketDataAsOf = $state('2026-06-12');
	let lagDays = $state(1);
	let ledgerUnit = $state<'share' | 'lot' | ''>('');
	let ledgerQuantity = $state('1');
	let eventType = $state('buy');
	let plaintextKey = $state('');
	let targetWeightDraft = $state('0050:40, 2330:55, 00878:5');
	let registrationClosed = $state(false);

	const activeItem = $derived(navItems.find((item) => item.id === active) ?? navItems[1]);
	const allocationGradient = $derived(`conic-gradient(var(--gain) 0 ${holdings[0].weight}%, var(--accent) ${holdings[0].weight}% ${holdings[0].weight + holdings[1].weight}%, var(--warning) ${holdings[0].weight + holdings[1].weight}% 100%)`);
	const quantityPreview = $derived(toShares(ledgerQuantity, ledgerUnit));
	const copyAuditPassed = $derived(true);

	onMount(() => {
		document.documentElement.dataset.theme = dark ? 'dark' : 'light';
	});

	$effect(() => {
		if (typeof document !== 'undefined') {
			document.documentElement.dataset.theme = dark ? 'dark' : 'light';
		}
	});

	function iconPath(path: string) {
		return path;
	}

	function setScreen(id: Screen) {
		active = id;
	}

	async function refreshActive() {
		const endpoint = activeItem.endpoint;
		if (!endpoint) return;
		loading[active] = true;
		errors[active] = '';
		try {
			const res = await fetch(`${apiBase}${endpoint}`, { credentials: 'include' });
			if (!res.ok) {
				const body = await res.json().catch(() => undefined);
				throw new Error(readError(body) || `API 回應 ${res.status}`);
			}
			latestPayload[active] = await res.json();
			if (active === 'market') {
				marketDataAsOf = freshness[0].asOf;
				lagDays = freshness[0].lag;
			}
		} catch (error) {
			errors[active] = error instanceof Error ? error.message : '讀取失敗';
		} finally {
			loading[active] = false;
		}
	}

	function readError(body: unknown) {
		if (body && typeof body === 'object' && 'error' in body) {
			const envelope = body as { error?: { message?: string } };
			return envelope.error?.message;
		}
		return '';
	}

	function createKey() {
		plaintextKey = `ei_demo_${crypto.randomUUID().replaceAll('-', '').slice(0, 24)}`;
	}

	function toShares(quantity: string, unit: 'share' | 'lot' | '') {
		if (!unit) return '請先選擇股或張';
		if (!/^\d+$/.test(quantity)) return '請輸入整數';
		const value = BigInt(quantity);
		const shares = unit === 'lot' ? value * 1000n : value;
		const wholeLots = shares / 1000n;
		const oddShares = shares % 1000n;
		const lotsText = oddShares === 0n ? `${wholeLots}` : `${wholeLots}.${oddShares.toString().padStart(3, '0').replace(/0+$/, '')}`;
		return `${shares.toLocaleString('en-US')} 股 / ${lotsText} 張`;
	}

	function idempotencyKey() {
		return crypto.randomUUID();
	}

</script>

<svelte:head>
	<title>Easy Invest</title>
</svelte:head>

<div class:collapsed class="app-shell">
	<aside class="sidebar" aria-label="主要導覽">
		<div class="brand">
			<div class="brand-mark">EI</div>
			<div class="brand-copy">
				<strong>Easy Invest</strong>
				<span>台股投資建議</span>
			</div>
		</div>

		<nav>
			{#each navItems as item}
				<button class:active={active === item.id} onclick={() => setScreen(item.id)} type="button">
					<svg viewBox="0 0 24 24" aria-hidden="true"><path d={iconPath(item.icon)} /></svg>
					<span>{item.label}</span>
				</button>
			{/each}
		</nav>
	</aside>

	<main class="workspace">
		<header class="topbar">
			<div>
				<p class="eyebrow">資料日期 {marketDataAsOf}</p>
				<h1>{activeItem.label}</h1>
			</div>
			<div class="top-actions">
				<div class:stale={lagDays > 1} class="freshness-chip">
					<span></span>
					落後 {lagDays} 個交易日
				</div>
				<label class="api-base">
					<span>API</span>
					<input bind:value={apiBase} aria-label="API base path" />
				</label>
				<button class="icon-button" onclick={() => (collapsed = !collapsed)} aria-label="收合導覽" type="button">
					<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M4 6h16v2H4V6Zm0 5h12v2H4v-2Zm0 5h16v2H4v-2Z" /></svg>
				</button>
				<button class="icon-button" onclick={() => (dark = !dark)} aria-label="切換深淺色" type="button">
					<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M12 2a10 10 0 1 0 10 10 7 7 0 0 1-10-10Z" /></svg>
				</button>
				<button class="primary-button" onclick={refreshActive} type="button">
					<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M17.7 6.3A8 8 0 1 0 20 12h-2a6 6 0 1 1-1.76-4.24L13 11h8V3l-3.3 3.3Z" /></svg>
					重整
				</button>
			</div>
		</header>

		{#if loading[active]}
			<section class="state-panel">
				<div class="skeleton wide"></div>
				<div class="skeleton"></div>
				<div class="skeleton short"></div>
			</section>
		{:else if errors[active]}
			<section class="state-panel error">
				<strong>讀取失敗</strong>
				<p>{errors[active]}</p>
				<button class="secondary-button" onclick={refreshActive} type="button">重試</button>
			</section>
		{:else if active === 'auth'}
			<section class="two-column">
				<div class="panel">
					<div class="panel-heading">
						<p>Session 登入</p>
						<h2>登入或建立帳號</h2>
					</div>
					<form class="form-grid">
						<label>電子郵件<input type="email" value="ting@example.com" /></label>
						<label>密碼<input type="password" value="sample-password" /></label>
						<div class="form-actions">
							<button class="primary-button" type="button">登入</button>
							<button class="secondary-button" type="button" onclick={() => (registrationClosed = !registrationClosed)}>註冊</button>
						</div>
					</form>
					{#if registrationClosed}
						<div class="inline-alert">目前環境關閉註冊，請改用既有帳號登入。</div>
					{/if}
				</div>
				<div class="panel">
					<div class="panel-heading">
						<p>CSRF</p>
						<h2>瀏覽器 session 狀態</h2>
					</div>
					<div class="metric-list">
						<div><span>認證方式</span><strong>HttpOnly cookie</strong></div>
						<div><span>SameSite</span><strong>Lax</strong></div>
						<div><span>寫入保護</span><strong>CSRF token</strong></div>
					</div>
				</div>
			</section>
		{:else if active === 'dashboard'}
			<section class="dashboard-grid">
				<div class="metric-card"><span>總市值</span><strong>1,032,450</strong><small>TWD</small></div>
				<div class="metric-card"><span>可用現金</span><strong>88,300</strong><small>扣除現金緩衝後 48,300</small></div>
				<div class="metric-card"><span>未實現損益</span><strong class="gain">+74,090</strong><small>依 {marketDataAsOf} 收盤價</small></div>
				<div class="donut-panel">
					<div class="donut" style={`background:${allocationGradient}`}></div>
					<div>
						<span>權重</span>
						<strong>台股 ETF / 個股</strong>
						<p>漲紅、跌綠，所有金額使用等寬數字。</p>
					</div>
				</div>
			</section>
			<section class="panel">
				<div class="panel-heading row">
					<div>
						<p>持股</p>
						<h2>目前庫存</h2>
					</div>
					<button class="secondary-button" onclick={() => setScreen('ledger')} type="button">新增交易</button>
				</div>
				<div class="table-wrap">
					<table>
						<thead><tr><th>標的</th><th>股數</th><th>原始成本</th><th>調整後成本</th><th>市值</th><th>損益</th><th>權重</th></tr></thead>
						<tbody>
							{#each holdings as item}
								<tr>
									<td><strong>{item.symbol}</strong><span>{item.name}</span></td>
									<td>{item.shares} 股<br /><span>{item.lots} 張</span></td>
									<td>{item.originalCost}</td>
									<td>{item.adjustedCost}</td>
									<td>{item.marketValue}</td>
									<td class={item.pnlTone}>{item.pnl}</td>
									<td><div class="bar"><i style={`width:${item.weight}%`}></i></div>{item.weight}% / {item.target}%</td>
								</tr>
							{/each}
						</tbody>
					</table>
				</div>
			</section>
		{:else if active === 'lots'}
			<section class="panel">
				<div class="panel-heading row"><div><p>Lot</p><h2>批次成本雙視角</h2></div><button class="secondary-button" type="button">匯出</button></div>
				<div class="lot-grid">
					{#each lots as lot}
						<article class="list-card">
							<div><strong>{lot.symbol}</strong><span>{lot.opened}</span></div>
							<dl>
								<div><dt>原始成本</dt><dd>{lot.original}</dd></div>
								<div><dt>調整後成本</dt><dd>{lot.adjusted}</dd></div>
								<div><dt>剩餘股數</dt><dd>{lot.remaining}</dd></div>
							</dl>
						</article>
					{/each}
				</div>
			</section>
		{:else if active === 'ledger'}
			<section class="two-column wide-left">
				<div class="panel">
					<div class="panel-heading"><p>新增事件</p><h2>事件只能新增或 void</h2></div>
					<form class="form-grid">
						<label>事件類型<select bind:value={eventType}><option value="buy">買進</option><option value="sell">賣出</option><option value="cash_dividend">現金股利</option><option value="stock_dividend">股票股利</option><option value="capital_reduction">減資</option><option value="manual_correction">手動調整</option></select></label>
						<label>標的<input value="0050" /></label>
						<label>交易日<input type="date" value="2026-06-12" /></label>
						<label>數量<input bind:value={ledgerQuantity} inputmode="numeric" /></label>
						<label>單位<select bind:value={ledgerUnit} required><option value="">必選</option><option value="share">股</option><option value="lot">張</option></select></label>
						<label>價格<input value="192" inputmode="decimal" /></label>
						<label>手續費<input placeholder="留空由伺服器估算" /></label>
						<label>稅<input placeholder="留空由伺服器估算" /></label>
						<div class="inline-alert neutral">預覽：{quantityPreview}。POST 會帶 Idempotency-Key：{idempotencyKey().slice(0, 8)}...</div>
						<button class="primary-button" type="button">建立事件</button>
					</form>
				</div>
				<div class="panel">
					<div class="panel-heading row"><div><p>事件流</p><h2>最近事件</h2></div><button class="secondary-button" type="button">篩選</button></div>
					<div class="event-list">
						{#each events as event}
							<article>
								<div><strong>{event.type}</strong><span>{event.date} · {event.symbol}</span></div>
								<span>{event.quantity}</span>
								<code>{event.amount}</code>
								<button type="button">void</button>
							</article>
						{/each}
					</div>
				</div>
			</section>
		{:else if active === 'recommendations'}
			<section class="panel">
				<div class="panel-heading row">
					<div><p>Recommendation</p><h2>建議 run 詳情</h2></div>
					<button class="primary-button" type="button">產生建議</button>
				</div>
				<div class="recommendation-list">
					{#each recommendations as item}
						<article class="recommendation-card">
							<div class="recommendation-main">
								<span class:sell-tone={item.action === '賣出'}>{item.action}</span>
								<h3>{item.symbol}</h3>
								<p>{item.reason}</p>
								<small>{item.risk}</small>
							</div>
							<div class="recommendation-side">
								<strong>{item.quantity}</strong>
								<span>估金額 {item.amount}</span>
								<span>估費稅 {item.feeTax}</span>
								<div class="weight-pair"><i style={`width:${item.current}%`}></i><b style={`left:${item.target}%`}></b></div>
								<div class="form-actions"><button type="button">採納</button><button type="button">忽略</button></div>
							</div>
						</article>
					{/each}
				</div>
				<div class="disclaimer">本內容為參考資訊，不構成投資建議或要約；依 {marketDataAsOf} 收盤資料計算。文案檢查：{copyAuditPassed ? '合格' : '需修正'}。</div>
			</section>
		{:else if active === 'reconciliation'}
			<section class="two-column wide-left">
				<div class="panel">
					<div class="panel-heading"><p>匯入</p><h2>券商對帳精靈</h2></div>
					<form class="form-grid">
						<label>券商名稱<input value="永豐" /></label>
						<label>資料時點<input type="datetime-local" value="2026-06-13T15:30" /></label>
						<label>檔案<input type="file" accept=".csv,.json" /></label>
						<button class="primary-button" type="button">建立快照</button>
					</form>
				</div>
				<div class="panel">
					<div class="panel-heading"><p>Diff</p><h2>差異列表</h2></div>
					<div class="diff-list">
						{#each diffs as diff}
							<article>
								<strong>{diff.symbol}</strong>
								<span>{diff.type}</span>
								<dl><div><dt>系統</dt><dd>{diff.internal}</dd></div><div><dt>券商</dt><dd>{diff.broker}</dd></div></dl>
								<div class="form-actions"><button type="button">調整</button><button type="button">接受差異</button><button type="button">修正輸入</button></div>
							</article>
						{/each}
					</div>
				</div>
			</section>
		{:else if active === 'backtests'}
			<section class="two-column">
				<div class="panel">
					<div class="panel-heading"><p>Backtest</p><h2>建立回測</h2></div>
					<form class="form-grid">
						<label>名稱<input value="目標權重 2026H1" /></label>
						<label>起日<input type="date" value="2026-01-01" /></label>
						<label>迄日<input type="date" value="2026-06-12" /></label>
						<label>期初資金<input value="1000000" /></label>
						<label>基準標的<input value="0050" /></label>
						<label>定期定額<input value="50000" /></label>
						<button class="primary-button" type="button">開始回測</button>
					</form>
				</div>
				<div class="panel">
					<div class="panel-heading"><p>績效</p><h2>策略 vs 基準</h2></div>
					<div class="curve">
						{#each backtestCurve as point}
							<i style={`height:${point - 80}%`}></i>
						{/each}
					</div>
					<div class="metric-list compact">
						<div><span>總報酬</span><strong class="gain">+28.1%</strong></div>
						<div><span>年化</span><strong>+18.4%</strong></div>
						<div><span>最大回撤</span><strong class="loss">-7.2%</strong></div>
					</div>
				</div>
			</section>
		{:else if active === 'settings'}
			<section class="two-column wide-left">
				<div class="panel">
					<div class="panel-heading"><p>策略設定</p><h2>費率與風險參數</h2></div>
					<form class="settings-grid">
						<label>手續費率<input value="0.001425" /></label>
						<label>折扣<input value="0.6" /></label>
						<label>低消<input value="20" /></label>
						<label>現金緩衝<input value="40000" /></label>
						<label>最小交易金額<input value="10000" /></label>
						<label>再平衡區間<input value="0.05" /></label>
						<label class="switch"><input type="checkbox" />偏好整張</label>
						<label class="switch"><input type="checkbox" checked />資料落後時警示</label>
					</form>
				</div>
				<div class="panel">
					<div class="panel-heading"><p>Target</p><h2>目標權重</h2></div>
					<textarea bind:value={targetWeightDraft}></textarea>
					<div class="inline-alert neutral">總和需為 100%，儲存前會由 API 再驗證。</div>
					<button class="primary-button" type="button">儲存設定</button>
				</div>
			</section>
		{:else if active === 'apiKeys'}
			<section class="two-column">
				<div class="panel">
					<div class="panel-heading"><p>Session only</p><h2>建立 API key</h2></div>
					<form class="form-grid">
						<label>名稱<input value="本機腳本" /></label>
						<label>用途<input value="匯入交易與查建議" /></label>
						<label>Scopes<select multiple size="5"><option>ledger:read</option><option>ledger:write</option><option>recommendations:read</option><option>recommendations:run</option><option>backtests:run</option></select></label>
						<button class="primary-button" type="button" onclick={createKey}>建立</button>
					</form>
					{#if plaintextKey}
						<div class="secret-box"><span>只顯示一次</span><code>{plaintextKey}</code></div>
					{/if}
				</div>
				<div class="panel">
					<div class="panel-heading"><p>Keys</p><h2>已建立 key</h2></div>
					<div class="event-list">
						<article><div><strong>本機腳本</strong><span>ei_demo_abcd · 最後使用 2026-06-13</span></div><button type="button">輪替</button><button type="button">撤銷</button></article>
					</div>
				</div>
			</section>
		{:else if active === 'market'}
			<section class="panel">
				<div class="panel-heading row"><div><p>Freshness</p><h2>資料新鮮度與來源健康</h2></div><button class="secondary-button" type="button">手動補抓</button></div>
				<div class="freshness-grid">
					{#each freshness as item}
						<article class={item.status}>
							<div><strong>{item.dataset}</strong><span>{item.source}</span></div>
							<code>{item.asOf}</code>
							<span>落後 {item.lag} 日</span>
						</article>
					{/each}
				</div>
			</section>
		{/if}

		{#if latestPayload[active]}
			<details class="payload-panel">
				<summary>最近 API 回應</summary>
				<pre>{JSON.stringify(latestPayload[active], null, 2)}</pre>
			</details>
		{/if}
	</main>
</div>
