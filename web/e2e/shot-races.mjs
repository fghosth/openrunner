// 赛历页截图（人工核对用，不进 CI）。
//
// 用法：
//   ./dist/openrunner --addr :8093 --data /tmp/rv5   # 另开终端，数据里要有 race_events
//   node e2e/shot-races.mjs <token> <输出目录>
//
// 为什么手工：报名那一行的四种形态（正在报名 / 还没开放 / 已截止 / 没配上）
// 取决于上游当下的真实数据，e2e 里是打桩的，看不出真站数据长什么样。
import { chromium } from '@playwright/test'

const BASE = process.env.SHOT_BASE ?? 'http://localhost:8093'
const token = process.argv[2]
const outDir = process.argv[3] ?? '.'
if (!token) {
  console.error('用法: node e2e/shot-races.mjs <token> [输出目录]')
  process.exit(1)
}

const browser = await chromium.launch({ channel: 'chrome' })
const ctx = await browser.newContext({ viewport: { width: 1440, height: 1000 } })
await ctx.addInitScript((t) => {
  localStorage.setItem('openrunner.token', t)
  localStorage.setItem('openrunner.activeAthlete', '1')
}, token)
const page = await ctx.newPage()

// 不能用 networkidle：这个 SPA 会持续轮询，networkidle 永远不触发，脚本会挂死。
await page.goto(`${BASE}/races`, { waitUntil: 'domcontentloaded' })
await page.waitForTimeout(1500)
await page.screenshot({ path: `${outDir}/fix-01-map.png`, fullPage: false })

// 月份视图：报名行四种形态都在这里
const month = page.getByRole('button', { name: /月份|月/ }).first()
if (await month.count()) {
  await month.click()
  await page.waitForTimeout(1200)
  await page.screenshot({ path: `${outDir}/fix-02-month.png`, fullPage: false })
}

// 展开到能看到带报名信息的行
const expand = page.getByRole('button', { name: /展开全部|展开/ }).first()
if (await expand.count()) {
  await expand.click()
  await page.waitForTimeout(800)
}
await page.screenshot({ path: `${outDir}/fix-03-rows.png`, fullPage: false })

// 抓报名行的真实文本，确认四种形态的措辞
const lines = await page.$$eval('li', (els) =>
  els
    .map((e) => e.innerText.replace(/\s+/g, ' ').trim())
    .filter((s) => /报名|¥|复制/.test(s))
    .slice(0, 12),
)
console.log('=== 页面上真实的报名行 ===')
for (const l of lines) console.log(' ·', l.slice(0, 150))

await browser.close()
