import tailwindcss from '@tailwindcss/vite';
import { sveltekit } from '@sveltejs/kit/vite';
import { defineConfig } from 'vite';

export default defineConfig({
	plugins: [tailwindcss(), sveltekit()],
	server: {
		proxy: {
			// 開發時把 /api/ 代理到本機後端。
			// 注意要用 '/api/'（含斜線），否則會誤攔前端路由 /api-keys。
			'/api/': {
				target: 'http://localhost:8080',
				changeOrigin: true
			}
		}
	}
});
