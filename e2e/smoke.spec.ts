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

/**
 * 下面两条守卫针对「构建绿、无报错、但画面是空的」这类静默失败——
 * 现有用例（非白屏 + 无 JS 错误 + 无 5xx）对它们全部失明：
 *   - MapLibre 矢量瓦片事故：0 个瓦片请求、无报错、isStyleLoaded() 还返回 true
 *   - ECharts 6 markArea+dataZoom 组合内部异常：容器在、canvas 在，就是没画东西
 * 判据都取「真实产出」而不是 API 返回值（DEVLOG Round 12 教训）。
 */
test('轨迹墙：地图真的发起瓦片请求', async ({ page }) => {
  const assertClean = watch(page)
  // 瓦片打桩回 1x1 PNG，测试不依赖外网。Playwright 路由按注册的逆序匹配（LIFO），
  // 所以这条先于 beforeEach 的「外部全拦」生效。
  let tiles = 0
  await page.route('**webrd0*.is.autonavi.com/**', (route) => {
    tiles++
    route.fulfill({
      contentType: 'image/png',
      body: Buffer.from(
        'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==',
        'base64',
      ),
    })
  })
  await page.goto('/map', { waitUntil: 'domcontentloaded' })
  // 地图默认中心（上海 zoom 11）不依赖 GPS 数据就会请求瓦片；seed 无 GPS 也测得了
  await expect
    .poll(() => tiles, { timeout: 10_000, message: '地图一个瓦片请求都没发出去' })
    .toBeGreaterThan(0)
  assertClean()
})

test('趋势页：PMC 图真的画出像素', async ({ page }) => {
  const assertClean = watch(page)
  await page.goto('/trends', { waitUntil: 'domcontentloaded' })
  // 判据 = canvas 上非透明像素数。ECharts 内部异常时容器与 canvas 都在但全空白；
  // 地图页的 WebGL canvas 拿不到 2d context，会被 getContext 返回 null 自然跳过。
  const paintedPixels = () =>
    page.evaluate(() => {
      let best = 0
      for (const c of Array.from(document.querySelectorAll('canvas'))) {
        const ctx = c.getContext('2d')
        if (!ctx || c.width === 0) continue
        const { data } = ctx.getImageData(0, 0, c.width, c.height)
        let painted = 0
        for (let i = 3; i < data.length; i += 4) if (data[i] > 0) painted++
        if (painted > best) best = painted
      }
      return best
    })
  // 两条 CTL/ATL 折线 + 坐标轴文字/网格线，保守阈值 1000
  await expect
    .poll(paintedPixels, { timeout: 10_000, message: '趋势页 canvas 上没有画出内容' })
    .toBeGreaterThan(1000)
  assertClean()
})

test('下周计划校验面板：校验返回判定（票 03）', async ({ page }) => {
  const assertClean = watch(page)
  await page.goto('/', { waitUntil: 'domcontentloaded' })
  const panel = page.locator('section[aria-label="下周计划校验"]')
  await expect(panel).toBeVisible()
  // seed 里有一条下周计划课 → 校验按钮可用
  await panel.getByRole('button', { name: '校验' }).click()
  await expect(panel.locator('ul[aria-label="校验结果"] li').first()).toBeVisible({ timeout: 5000 })
  assertClean()
})

test('校验全 FAIL 时写入按钮禁用（变异检验形态）', async ({ page }) => {
  const assertClean = watch(page)
  // 拦截端点返回全 FAIL——票面要求的变异在测试内以路由复现，不动服务端
  await page.route('**/coach/week-check', (route) =>
    route.fulfill({
      contentType: 'application/json',
      body: JSON.stringify({
        verdicts: [{ level: 'FAIL', rule: 'volume', detail: 'mock fail', evidence: 'S3' }],
        week: { plan_start: '2026-01-05', week_role: 'build', runs: 1, total_min: 60, total_km: 10, long_min: 60, history_sessions: 0 },
      }),
    }),
  )
  await page.goto('/', { waitUntil: 'domcontentloaded' })
  const panel = page.locator('section[aria-label="下周计划校验"]')
  await panel.getByRole('button', { name: '加草稿' }).click()
  await panel.getByRole('button', { name: '校验' }).click()
  await expect(panel.getByText('mock fail')).toBeVisible({ timeout: 5000 })
  await expect(panel.getByRole('button', { name: /写入日历/ })).toBeDisabled()
  assertClean()
})

test('下周计划校验：日历计划课可删除（用户问题 2 回归）', async ({ page }) => {
  const assertClean = watch(page)
  await page.goto('/', { waitUntil: 'domcontentloaded' })
  const panel = page.locator('section[aria-label="下周计划校验"]')
  const calRow = panel.locator('li[data-act]').first()
  await expect(calRow).toBeVisible({ timeout: 5000 })
  const actId = await calRow.getAttribute('data-act')
  await calRow.getByRole('button', { name: '删除日历计划课' }).click()
  await expect(panel.locator(`li[data-act="${actId}"]`)).toHaveCount(0, { timeout: 5000 })
  assertClean()
})

test('下周计划校验：三连点写入只落一条（用户问题 1 回归）', async ({ page }) => {
  const assertClean = watch(page)
  await page.goto('/', { waitUntil: 'domcontentloaded' })
  const panel = page.locator('section[aria-label="下周计划校验"]')
  const before = await panel.locator('li[data-act]').count()
  await panel.getByRole('button', { name: '加草稿' }).click()
  await panel.getByRole('button', { name: '校验' }).click()
  const write = panel.getByRole('button', { name: /写入日历/ })
  await expect(write).toBeEnabled({ timeout: 5000 })
  // 同一任务里连点三次：复现 setBusy 异步生效前的双击竞态（修复前会写 3 条）
  await page.evaluate(() => {
    const btn = [...document.querySelectorAll('button')].find((b) => b.textContent?.includes('写入日历'))
    btn?.click()
    btn?.click()
    btn?.click()
  })
  await expect(panel.locator('li[data-act]')).toHaveCount(before + 1, { timeout: 5000 })
  assertClean()
})

test('设置页：训练配速表渲染七区 + VDOT（票 04）', async ({ page }) => {
  const assertClean = watch(page)
  await page.goto('/settings', { waitUntil: 'domcontentloaded' })
  const card = page.getByText('训练配速表', { exact: true })
  await expect(card).toBeVisible()
  // seed 了阈值配速 <占位> + LTHR <占位> + 10K 40:00 → 七区表与 VDOT 都应有值
  await expect(page.locator('td', { hasText: '乳酸阈' }).first()).toBeVisible({ timeout: 5000 })
  await expect(page.getByText(/VDOT \d/).first()).toBeVisible()
  assertClean()
})

test('趋势页：全马预测显示模型与推荐（票 05）', async ({ page }) => {
  const assertClean = watch(page)
  await page.goto('/trends', { waitUntil: 'domcontentloaded' })
  const head = page.getByText('全马预测（多模型对照）')
  await expect(head).toBeVisible({ timeout: 8000 })
  // seed 有 10K 40:00 + 周跑量 → Riegel 与 Vickers 至少两行，且有推荐徽章
  await expect(page.getByText('Riegel 1.06')).toBeVisible()
  await expect(page.getByText('Vickers 模型 1')).toBeVisible()
  await expect(page.getByText('推荐', { exact: true })).toBeVisible()
  assertClean()
})

test('活动详情页：单课分析块出现并给不适用原因（票 06）', async ({ page }) => {
  const assertClean = watch(page)
  // seed 跑步（无流、45 分钟）→ 应显示分析块 + 明确原因，而非空白或报错
  await page.goto('/activities/1', { waitUntil: 'domcontentloaded' })
  const sec = page.locator('section[aria-label="单课分析"]')
  await expect(sec).toBeVisible({ timeout: 8000 })
  await expect(sec.getByText(/不适用/)).toBeVisible()
  assertClean()
})
