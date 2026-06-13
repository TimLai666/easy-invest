import { auth, ApiError } from '$lib/api';
import type { User } from '$lib/api';

/*
  Session 狀態（Svelte 5 runes）。
  後端用 HttpOnly cookie，前端無法讀取 token，只能靠 /me 確認登入狀態。
*/

class SessionStore {
	user = $state<User | null>(null);
	scopes = $state<string[] | null>(null);
	authVia = $state<'session' | 'api_key' | null>(null);
	loading = $state(true);

	get isAuthed(): boolean {
		return this.user !== null;
	}

	async refresh(): Promise<void> {
		this.loading = true;
		try {
			const me = await auth.me();
			this.user = me.user;
			this.scopes = me.scopes;
			this.authVia = me.auth_via;
		} catch (e) {
			if (e instanceof ApiError && e.isAuth) {
				this.user = null;
				this.scopes = null;
				this.authVia = null;
			}
			// 非認證錯誤（例如後端未啟動）保留現況，由頁面顯示錯誤
		} finally {
			this.loading = false;
		}
	}

	async login(email: string, password: string): Promise<void> {
		const user = await auth.login({ email, password });
		this.user = user;
		await this.refresh();
	}

	async register(email: string, password: string, displayName: string): Promise<User> {
		return auth.register({ email, password, display_name: displayName });
	}

	async logout(): Promise<void> {
		try {
			await auth.logout();
		} finally {
			this.user = null;
			this.scopes = null;
			this.authVia = null;
		}
	}
}

export const session = new SessionStore();
