import adapter from '@sveltejs/adapter-static';
import { vitePreprocess } from '@sveltejs/vite-plugin-svelte';

/** @type {import('@sveltejs/kit').Config} */
const config = {
	preprocess: vitePreprocess(),
	kit: {
		// SPA 模式：API-first，全部路由前端渲染，由 nginx fallback 到 index.html
		adapter: adapter({ fallback: 'index.html', strict: false }),
		alias: {
			'@/*': './src/lib/*'
		}
	}
};

export default config;
