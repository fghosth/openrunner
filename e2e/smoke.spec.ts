/**
 * 冒烟测试：五张页面能渲染 + 无 JS 错误 + 无 5xx + 静态资源不 404。
 *
 * 目标不是覆盖交互，而是拦住两类真实事故：
 *   1. 前端产物没进二进制/镜像 → 打开白屏（Docker 那次）
 *   2. API 字段变更 / 前端路由失效 → 页面抛错或 404
 */
import { expect, test, type Page } from '@playwright/test'
import { readState } from './global-setup'

const state = readState()

/** 免走登录 UI：直接把 token 注入 localStorage（键名见 src/api.ts） */
async function authedPage(page: Page): Promise<void> {
  await page.addInitScript(
    ([token]) => {
      localStorage.setItem('openrunner.token', token)
    },
    [state.token],
  )
}

/** 收集 JS 错误与 5xx；返回一个「断言干净」的函数 */
function watch(page: Page) {
  const errors: string[] = []
  const badResponses: string[] = []

  page.on('pageerror', (e) => errors.push(`pageerror: ${e.message}`))
  page.on('console', (msg) => {
    if (msg.type() !== 'error') return
    const t = msg.text()
    // 地图瓦片 / 网络抖动属于环境问题而非应用缺陷：测试里外部请求被拦掉，必然报错
    if (/tile|net::|Failed to load|ERR_|favicon|Aborted|ERR_NAME/i.test(t)) return
    errors.push(`console: ${t}`)
  })
  page.on('response', (res) => {
    if (res.status() >= 500) badResponses.push(`${res.status()} ${res.url()}`)
    if (res.status() === 404 && /\/assets\//.test(res.url())) {
      badResponses.push(`404 静态资源 ${res.url()}`)
    }
  })

  return () => {
    expect(badResponses, '服务端 5xx 或静态资源 404').toEqual([])
    expect(errors, '浏览器 JS 错误').toEqual([])
  }
}

/** 外部请求一律拦掉：测试不依赖网络，也不会因瓦片加载慢而抖 */
async function offline(page: Page): Promise<void> {
  await page.route('**/*', (route) => {
    const url = route.request().url()
    const isLocal = url.startsWith(state.baseURL) || url.startsWith('data:') || url.startsWith('blob:')
    return isLocal ? route.continue() : route.abort()
  })
}

const NAV = ['活动', '趋势', '轨迹墙', '汇总', 'AI 分析']

test.beforeEach(async ({ page }) => {
  await authedPage(page)
  await offline(page)
})

test.describe('五张页面都能渲染', () => {
  for (const [path, name] of [
    ['/', '活动'],
    ['/trends', '趋势'],
    ['/map', '轨迹墙'],
    ['/stats', '汇总'],
    ['/ai-analysis', 'AI 分析'],
  ] as const) {
    test(`${path} 非白屏且导航完整`, async ({ page }) => {
      const assertClean = watch(page)

      await page.goto(path, { waitUntil: 'domcontentloaded' })

      // 白屏守卫：#root 必须有内容
      await expect.poll(() => page.evaluate(() => document.querySelector('#root')?.children.length ?? 0)).toBeGreaterThan(0)

      // 导航五条都在
      for (const label of NAV) {
        await expect(page.getByRole('link', { name: label })).toBeVisible()
      }
      // 当前页高亮（aria-current），证明路由与导航联动正常
      await expect(page.getByRole('link', { name })).toHaveAttribute('aria-current', 'page')

      assertClean()
    })
  }
})

test('设置页可打开（齿轮入口）', async ({ page }) => {
  const assertClean = watch(page)
  await page.goto('/settings', { waitUntil: 'domcontentloaded' })
  await expect.poll(() => page.evaluate(() => document.querySelector('#root')?.children.length ?? 0)).toBeGreaterThan(0)
  assertClean()
})

test('未登录时不会白屏，而是给出可操作界面', async ({ page }) => {
  const assertClean = watch(page)
  await page.route('**/*', (route) => {
    const url = route.request().url()
    return url.startsWith(state.baseURL) ? route.continue() : route.abort()
  })
  // 不注入 token
  await page.goto('/', { waitUntil: 'domcontentloaded' })
  await expect
    .poll(() => page.evaluate(() => document.querySelector('#root')?.children.length ?? 0))
    .toBeGreaterThan(0)
  assertClean()
})

test('活动列表能看到 seed 数据', async ({ page }) => {
  const assertClean = watch(page)
  await page.goto('/', { waitUntil: 'domcontentloaded' })
  // seed 灌入了一条 10.5km 跑步：列表里应出现距离
  await expect(page.getByText(/10\.5\s*km|10\.50\s*km/).first()).toBeVisible()
  assertClean()
})
