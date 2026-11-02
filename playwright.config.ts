import { defineConfig, devices } from '@playwright/test'

/**
 * openrunner 前端冒烟测试配置
 *
 * 默认测的是**生产形态**：Go 单二进制（内嵌前端产物）——因为出过的那次事故
 * 正是「前端产物没进镜像」，只有测内嵌产物才拦得住。
 *
 * 服务由 e2e/global-setup.ts 自己拉起（独立临时数据目录，绝不碰 ~/.openrunner）。
 *
 * 想在 Vite 热更新下跑（只改 UI 时更快）：
 *   make run                 # 另开一个终端起后端（:8080）
 *   npm run dev              # Vite（:5173，已代理 /api → :8080）
 *   E2E_BASE_URL=http://localhost:5173 npx playwright test
 */
export default defineConfig({
  testDir: './e2e',
  globalSetup: './e2e/global-setup.ts', // 自起隔离实例 + seed，跑完自动回收
  fullyParallel: false, // 共用一个服务端实例，串行更稳
  workers: 1,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 1 : 0,
  reporter: process.env.CI ? [['list'], ['html', { open: 'never' }]] : 'list',
  timeout: 30_000,
  expect: { timeout: 8_000 },

  use: {
    baseURL: process.env.E2E_BASE_URL ?? 'http://localhost:18080',
    trace: 'retain-on-failure', // 失败留 trace：npx playwright show-trace 可倒带
    screenshot: 'only-on-failure',
    video: 'off',
  },

  projects: [
    {
      name: 'chromium',
      use: {
        ...devices['Desktop Chrome'],
        // 默认复用本机已装的 Google Chrome —— 免下载 ~150MB 浏览器内核。
        // 想用 Playwright 自带的 Chromium：E2E_CHANNEL=bundled npx playwright test
        ...(process.env.E2E_CHANNEL === 'bundled' ? {} : { channel: 'chrome' }),
      },
    },
  ],
})
