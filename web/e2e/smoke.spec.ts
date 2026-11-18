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

const NAV = ['活动', '趋势', '轨迹墙', '赛事地图', '汇总', 'AI 分析']

test.beforeEach(async ({ page }) => {
  await authedPage(page)
  await offline(page)
})

test.describe('六张页面都能渲染', () => {
  for (const [path, name] of [
    ['/', '活动'],
    ['/trends', '趋势'],
    ['/map', '轨迹墙'],
    ['/races', '赛事地图'],
    ['/stats', '汇总'],
    ['/ai-analysis', 'AI 分析'],
  ] as const) {
    test(`${path} 非白屏且导航完整`, async ({ page }) => {
      const assertClean = watch(page)

      await page.goto(path, { waitUntil: 'domcontentloaded' })

      // 白屏守卫：#root 必须有内容
      await expect.poll(() => page.evaluate(() => document.querySelector('#root')?.children.length ?? 0)).toBeGreaterThan(0)

      // 导航六条都在
      for (const label of NAV) {
        await expect(page.getByRole('link', { name: label })).toBeVisible()
      }
      // 当前页高亮（aria-current），证明路由与导航联动正常
      await expect(page.getByRole('link', { name })).toHaveAttribute('aria-current', 'page')

      assertClean()
    })
  }
})
test('赛事地图：省级图块渲染，空库给出可操作提示', async ({ page }) => {
  const assertClean = watch(page)
  await page.goto('/races', { waitUntil: 'domcontentloaded' })

  // 边界是本地静态资源，离线也能画出来：省界 path 数量应接近省级要素个数
  await expect
    .poll(() => page.locator('svg[aria-label="中国省级马拉松赛事分布图"] path').count())
    .toBeGreaterThan(30)

  // e2e 关掉了赛历调度（见 global-setup），所以这里必然是空库状态
  await expect(page.getByText('还没有赛历数据')).toBeVisible()
  await expect(page.getByRole('button', { name: '立即更新' })).toBeVisible()

  assertClean()
})

test('赛事地图：点省份下钻到市/区与比赛列表', async ({ page }) => {
  const assertClean = watch(page)
  // 真实赛历要联网才能拉，这里回放一份最小树：省→市→区 + 三条比赛
  // 日期一律相对「今天」算，否则测试会在某个凌晨莫名其妙地红
  const inDays = (n: number) => new Date(Date.now() + n * 86400000).toISOString().slice(0, 10)
  const summary = {
    provinces: [
      {
        name: '江苏省',
        count: 2,
        cities: [{ name: '南京市', count: 2, districts: [{ name: '玄武区', count: 1 }] }],
      },
    ],
    years: [new Date().getFullYear()],
    grades: [
      { name: 'A', count: 1 },
      { name: '未标注', count: 1 },
    ],
    total: 2,
    lastSyncAt: 1794881181,
    lastError: '',
  }
  const list = [
    {
      raceId: '1',
      name: '南京马拉松',
      date: inDays(200),
      year: new Date().getFullYear(),
      grade: 'A',
      province: '江苏省',
      city: '南京市',
      district: '',
      categories: 'full',
      scale: '',
    },
    {
      raceId: '2',
      name: '玄武湖 10K',
      date: inDays(-200),
      year: new Date().getFullYear(),
      grade: '',
      province: '江苏省',
      city: '南京市',
      district: '玄武区',
      categories: '10k',
      scale: '',
    },
    {
      raceId: '3',
      name: '紫金山半马',
      date: inDays(10),
      year: new Date().getFullYear(),
      grade: 'B',
      province: '江苏省',
      city: '南京市',
      district: '',
      categories: 'half',
      scale: '',
      // 报名信息是外挂的（来源最酷）：配上的走截止+价格，没配上的走复制入口
      registration: {
        source: 'zuicool',
        sourceId: '9001',
        regStart: '2026-10-01T09:00:00',
        regEnd: inDays(5) + 'T23:59:00',
        minPrice: 180,
        regUrl: 'https://reg.zuicool.com/9001/1',
        fetchedAt: 1794881181,
      },
    },
  ]
  await page.route('**/api/races**', (route) => {
    const url = route.request().url()
    const body = url.includes('/api/races/summary') ? JSON.stringify(summary) : JSON.stringify(list)
    return route.fulfill({ contentType: 'application/json', body })
  })

  await page.goto('/races', { waitUntil: 'domcontentloaded' })
  const svg = page.locator('svg[aria-label="中国省级马拉松赛事分布图"]')
  await expect(svg.locator('path')).not.toHaveCount(0)
  // 级别选项来自 summary（上游取值会漂移，前端不写死）
  // 级别现在是左栏 chip 组（不再是 select），取值仍由 summary 渲染
  await expect(page.getByRole('button', { name: 'A · 1' })).toBeVisible()
  await expect(page.getByRole('button', { name: '未标注 · 1' })).toBeVisible()

  // 点江苏省的图块
  const hit = await page.evaluate(() => {
    const paths = [...document.querySelectorAll('svg[aria-label="中国省级马拉松赛事分布图"] path')]
    const i = paths.findIndex((p) => p.querySelector('title')?.textContent?.includes('江苏省'))
    if (i < 0) return null
    const r = paths[i].getBoundingClientRect()
    return { x: r.x + r.width / 2, y: r.y + r.height / 2 }
  })
  expect(hit, '江苏省图块应可定位（边界数据与 summary 的省名对得上）').not.toBeNull()
  await page.mouse.click(hit!.x, hit!.y)

  // 面板：市列表 + 比赛列表
  await expect(page.getByRole('button', { name: /南京/ })).toBeVisible()
  await expect(page.getByText('南京马拉松')).toBeVisible()
  await expect(page.getByText('玄武湖 10K')).toBeVisible()
  // 状态标记：未开始 / 即将开始（≤30 天）/ 已结束，日期都是相对今天造的
  // 紫金山半马同时出现在「即将开赛」横滚条与右侧列表里，取第一个即可
  await expect(page.getByText('紫金山半马').first()).toBeVisible()
  await expect(page.getByText(/未开始 · \d+ 天后/)).toBeVisible()
  await expect(page.getByText(/即将开始 · \d+ 天后/)).toBeVisible()
  await expect(page.getByText('已结束')).toBeVisible()
  // 配上了报名信息的：显示截止日期与起价，并给直达报名页的链接
  await expect(page.getByText(/报名截止 \d{2}-\d{2} 23:59/).first()).toBeVisible()
  await expect(page.getByText('¥180 起').first()).toBeVisible()
  await expect(page.getByRole('link', { name: '去最酷报名' }).first()).toBeVisible()
  // 没配上的：不显示空价格，退到复制入口
  await expect(page.getByRole('button', { name: '复制赛事名去官方渠道报名' }).first()).toBeVisible()

  // 下钻到市：出现区，列表收窄
  await page.getByRole('button', { name: /南京/ }).click()
  // 区 chip 与列表里的「南京市 · 玄武区」都含这个词，锚在 chip 上
  await expect(page.getByText(/^玄武区/)).toBeVisible()
  await expect(page.getByRole('button', { name: /南京/ })).toHaveCount(0)

  assertClean()
})


test('赛事地图：即将开赛横滚条、月份手风琴与赛历月视图', async ({ page }) => {
  const assertClean = watch(page)
  const inDays = (n: number) => new Date(Date.now() + n * 86400000).toISOString().slice(0, 10)
  const soon = inDays(10)
  const list = [
    {
      raceId: '1',
      name: '南京马拉松',
      date: inDays(200),
      year: new Date().getFullYear(),
      grade: 'A',
      province: '江苏省',
      city: '南京市',
      district: '',
      categories: 'full',
      scale: '',
    },
    {
      raceId: '3',
      name: '紫金山半马',
      date: soon,
      year: new Date().getFullYear(),
      grade: 'B',
      province: '江苏省',
      city: '南京市',
      district: '',
      categories: 'half',
      scale: '',
    },
  ]
  const summary = {
    provinces: [
      {
        name: '江苏省',
        count: 2,
        cities: [{ name: '南京市', count: 2, districts: [] }],
      },
    ],
    years: [new Date().getFullYear()],
    grades: [{ name: 'A', count: 1 }],
    total: 2,
    lastSyncAt: 1794881181,
    lastError: '',
  }
  await page.route('**/api/races**', (route) => {
    const url = route.request().url()
    const body = url.includes('/api/races/summary') ? JSON.stringify(summary) : JSON.stringify(list)
    return route.fulfill({ contentType: 'application/json', body })
  })
  await page.goto('/races', { waitUntil: 'domcontentloaded' })

  // 首屏横滚条：90 天内的那场进「即将开赛」，200 天后的那场不该进
  await expect(page.getByText('即将开赛')).toBeVisible()
  await expect(page.getByText('紫金山半马').first()).toBeVisible()
  await expect(page.getByText('南京马拉松')).toHaveCount(0)

  // 月份视图：默认展开「最近一场未开始」所在月，展开全部后远处那场才出现
  await page.getByRole('button', { name: '月份', exact: true }).click()
  await expect(page.getByRole('button', { name: /月赛事/ }).first()).toBeVisible()
  await expect(page.getByText('南京马拉松')).toHaveCount(0)
  await page.getByRole('button', { name: '展开全部' }).click()
  await expect(page.getByText('南京马拉松')).toBeVisible()

  // 赛历视图：一格一天，点有场次的日期看当天明细
  await page.getByRole('button', { name: '赛历', exact: true }).click()
  await page.getByRole('button', { name: new RegExp(soon) }).click()
  await expect(page.getByText('紫金山半马').first()).toBeVisible()

  assertClean()
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

test('汇总页不计入计划课（计划/实际分流）', async ({ page }) => {
  const assertClean = watch(page)
  await page.goto('/stats', { waitUntil: 'domcontentloaded' })
  await page.getByRole('button', { name: 'Life' }).click()
  // seed：run + ride + 4 条历史 = 6 条实际；另有 1 条下周计划课不得计入
  const counts = page.getByText('活动次数', { exact: true })
  await expect(counts.first()).toBeVisible({ timeout: 5000 })
  const card = counts.locator('xpath=ancestor::div[1]')
  await expect(card.first().locator('dd')).toHaveText('6')
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
  // 草稿行课型改成 tempo（行内 select 是第二个「课型」，第一个是添加编辑器）
  const rowType = panel.locator('li').getByLabel('课型')
  await rowType.selectOption('tempo')
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
  // 课型必须跟着落库——回归「写入日历后全变 easy」
  await expect(panel.locator('li[data-act]').last().getByLabel('课型')).toHaveValue('tempo')
  assertClean()
})

test('设置页：训练配速表渲染七区 + VDOT（票 04）', async ({ page }) => {
  const assertClean = watch(page)
  await page.goto('/settings', { waitUntil: 'domcontentloaded' })
  const card = page.getByText('训练配速表', { exact: true })
  await expect(card).toBeVisible()
  // seed 了阈值配速 + LTHR + 10K 40:00 → 七区表与 VDOT 都应有值
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

test('术语提示：悬停术语弹出说明，移开关闭，且说明文字自身不再挂术语', async ({ page }) => {
  const assertClean = watch(page)
  await page.goto('/trends', { waitUntil: 'domcontentloaded' })
  const term = page.locator('h1 span[tabindex="0"]').first()
  await expect(term).toBeVisible({ timeout: 8000 })
  const label = (await term.innerText()).trim()
  expect(label.length).toBeGreaterThan(0)

  await term.hover()
  const tip = page.locator('[role="tooltip"]')
  await expect(tip).toBeVisible()
  // 标题就是那个术语，正文是释义（术语表里每条都有 zh 解释）
  await expect(tip).toContainText(label)
  expect(((await tip.innerText()).length)).toBeGreaterThan(label.length + 8)
  // 释义里出现的 CTL/体能 等词不能再被标记，否则 tooltip 里套 tooltip
  expect(await tip.locator('span[tabindex="0"]').count()).toBe(0)

  await page.locator('main').hover()
  await expect(tip).toHaveCount(0)
  assertClean()
})

test('术语提示：键盘可聚焦，Esc 关闭', async ({ page }) => {
  const assertClean = watch(page)
  await page.goto('/trends', { waitUntil: 'domcontentloaded' })
  const term = page.locator('h1 span[tabindex="0"]').first()
  await expect(term).toBeVisible({ timeout: 8000 })
  await term.focus()
  const tip = page.locator('[role="tooltip"]')
  await expect(tip).toBeVisible()
  await page.keyboard.press('Escape')
  await expect(tip).toHaveCount(0)
  assertClean()
})
