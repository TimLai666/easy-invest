import LayoutDashboardIcon from '@lucide/svelte/icons/layout-dashboard';
import LayersIcon from '@lucide/svelte/icons/layers';
import ReceiptTextIcon from '@lucide/svelte/icons/receipt-text';
import LightbulbIcon from '@lucide/svelte/icons/lightbulb';
import ScaleIcon from '@lucide/svelte/icons/scale';
import LineChartIcon from '@lucide/svelte/icons/chart-line';
import SettingsIcon from '@lucide/svelte/icons/settings';
import KeyRoundIcon from '@lucide/svelte/icons/key-round';
import DatabaseIcon from '@lucide/svelte/icons/database';
import type { Component } from 'svelte';

export interface NavItem {
	href: string;
	label: string;
	icon: Component;
	group: '總覽' | '帳務' | '決策' | '系統';
}

export const navItems: NavItem[] = [
	{ href: '/', label: '儀表板', icon: LayoutDashboardIcon, group: '總覽' },
	{ href: '/transactions', label: '交易流水', icon: ReceiptTextIcon, group: '帳務' },
	{ href: '/lots', label: '批次明細', icon: LayersIcon, group: '帳務' },
	{ href: '/reconciliation', label: '券商對帳', icon: ScaleIcon, group: '帳務' },
	{ href: '/recommendations', label: '投資建議', icon: LightbulbIcon, group: '決策' },
	{ href: '/backtests', label: '策略回測', icon: LineChartIcon, group: '決策' },
	{ href: '/market', label: '市場資料', icon: DatabaseIcon, group: '系統' },
	{ href: '/settings', label: '設定', icon: SettingsIcon, group: '系統' },
	{ href: '/api-keys', label: 'API 金鑰', icon: KeyRoundIcon, group: '系統' }
];

export const navGroups = ['總覽', '帳務', '決策', '系統'] as const;

export const pageTitles: Record<string, string> = Object.fromEntries(
	navItems.map((n) => [n.href, n.label])
);

/** 圖表配色（依序取用） */
export const chartPalette = [
	'var(--chart-1)',
	'var(--chart-2)',
	'var(--chart-3)',
	'var(--chart-4)',
	'var(--chart-5)',
	'var(--info)',
	'var(--warning)'
];
