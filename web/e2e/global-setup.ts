/**
 * 全局前置：拉起一个**隔离**的 openrunner 实例并灌入种子数据。
 *
 * 隔离保证：
 *   - 独立端口 18080（你日常在跑的 :8080 不受影响）
 *   - 独立数据目录（os.tmpdir()/openrunner-e2e，每次跑前清空）
 *   - 绝不读写 ~/.openrunner
 *
 * 若显式设了 E2E_BASE_URL（例如指向 Vite dev server），则不再自己起服务，
 * 但仍会在目标实例上做一次幂等 seed（已初始化就跳过 setup）。
 */
import { spawn, type ChildProcess } from 'node:child_process'
import { existsSync, mkdirSync, readFileSync, rmSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'

const BASE = process.env.E2E_BASE_URL ?? 'http://localhost:18080'
const SPAWNED = !process.env.E2E_BASE_URL // 只有默认模式才由本文件负责起停
const BIN = process.env.OPENRUNNER_BIN ?? '../dist/openrunner'
const DATA_DIR = join(tmpdir(), 'openrunner-e2e')
const STATE_FILE = join(import.meta.dirname, '.auth-state.json')

const CREDS = { username: 'e2e', password: 'e2e-pass-123' } // 密码至少 6 位（store.minPasswordLen）

export interface E2EState {
  baseURL: string
  token: string
  athleteId: number
}

export default async function globalSetup(): Promise<(() => Promise<void>) | void> {
  let child: ChildProcess | undefined

  if (SPAWNED) {
    const binAbs = join(process.cwd(), BIN)
    if (!existsSync(binAbs)) {
      throw new Error(
        `找不到可执行文件 ${binAbs}\n` +
          '  → 先在 openrunner 仓库执行： make ui-dist && make build（或直接 make e2e）',
      )
    }
    rmSync(DATA_DIR, { recursive: true, force: true })
    mkdirSync(DATA_DIR, { recursive: true })

    child = spawn(binAbs, ['--addr', ':18080', '--data', DATA_DIR], {
      stdio: ['ignore', 'pipe', 'pipe'],
    })
    const log: string[] = []
    child.stdout?.on('data', (d) => log.push(String(d)))
    child.stderr?.on('data', (d) => log.push(String(d)))

    await waitForHealth(`${BASE}/health`, 15_000, () => {
      throw new Error(`服务未在 15s 内就绪。输出：\n${log.join('')}`)
    })
  } else {
    await waitForHealth(`${BASE}/health`, 10_000, () => {
      throw new Error(
        `E2E_BASE_URL=${BASE} 不可用。用 Vite 模式需要同时开着后端(:8080) 和 Vite(:5173)。`,
      )
    })
  }

  const state = await seed(BASE)
  writeFileSync(STATE_FILE, JSON.stringify(state))

  if (!child) return
  const proc = child
  return async () => {
    proc.kill('SIGTERM')
    rmSync(DATA_DIR, { recursive: true, force: true })
  }
}

/** 幂等 seed：建管理员（若首次）→ 建 Athlete → 灌两条活动（跑步 + 骑行） */
async function seed(base: string): Promise<E2EState> {
  const status = await json<{ needsSetup: boolean }>(`${base}/api/auth/status`)

  let token: string
  if (status.needsSetup) {
    const r = await json<{ token: string }>(`${base}/api/auth/setup`, {
      method: 'POST',
      body: JSON.stringify(CREDS),
    })
    token = r.token
  } else {
    const r = await json<{ token: string }>(`${base}/api/auth/login`, {
      method: 'POST',
      body: JSON.stringify(CREDS),
    })
    token = r.token
  }

  const auth = { Authorization: `Bearer ${token}` }
  const athletes = await json<{ id: number }[]>(`${base}/api/athletes`, { headers: auth })
  let athleteId = athletes[0]?.id
  if (!athleteId) {
    const created = await json<{ id: number }>(`${base}/api/athletes`, {
      method: 'POST',
      headers: auth,
      body: JSON.stringify({ name: 'E2E Runner' }),
    })
    athleteId = created.id
  }

  // 两条活动：让首页/汇总/趋势/轨迹墙都有数据可渲染（字段名对照 server/api/activities.go）
  const seedRun = { date: isoDate(0), sport: 'run', durationSecs: 2700, distanceM: 10500, avgHr: 152 }
  // 历史周跑量：预测的周跑量只算已完成的周（当前周排除），seed 必须铺过去 4 周
  const seedHistory = [7, 14, 21, 28].map((d) => ({
    date: isoDate(d), sport: 'run', durationSecs: 3600, distanceM: 12000, avgHr: 150,
  }))
  const seedRide = { date: isoDate(2), sport: 'ride', durationSecs: 3600, distanceM: 32000, avgHr: 138 }
  // 下周计划课：喂「下周计划校验」面板（票 03）
  const seedPlanned = { date: isoDate(-7), sport: 'run', durationSecs: 3600, distanceM: 10000, isPlanned: true }
  const existing = await json<unknown[]>(`${base}/api/athletes/${athleteId}/activities`, {
    headers: auth,
  })
  if (existing.length === 0) {
    for (const body of [seedRun, seedRide, seedPlanned, ...seedHistory]) {
      const res = await fetch(`${base}/api/athletes/${athleteId}/activities`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', ...auth },
        body: JSON.stringify(body),
      })
      if (!res.ok) throw new Error(`seed 活动失败 ${res.status}: ${await res.text()}`)
    }
    // 阈值/LTHR/比赛成绩：喂「训练配速表」与「全马预测」（票 04/05）
    const extras: [string, unknown, string][] = [
      [`/api/athletes/${athleteId}/pace-zones`, { validFrom: '2000-01-01', validTo: '9999-12-31', thresholdPaceSecsPerKm: 300 }, 'PUT'],
      [`/api/athletes/${athleteId}/hr-zones`, { validFrom: '2000-01-01', validTo: '9999-12-31', maxHr: 190, restHr: 60, ltHr: 170 }, 'PUT'],
      [`/api/athletes/${athleteId}/race-results`, { date: '2026-01-10', distanceM: 10000, timeSecs: 2400 }, 'POST'],
    ]
    for (const [path, body, method] of extras) {
      const res = await fetch(`${base}${path}`, {
        method,
        headers: { 'Content-Type': 'application/json', ...auth },
        body: JSON.stringify(body),
      })
      if (!res.ok) throw new Error(`seed ${path} 失败 ${res.status}: ${await res.text()}`)
    }
  }

  return { baseURL: base, token, athleteId }
}

function isoDate(daysAgo: number): string {
  const d = new Date()
  d.setDate(d.getDate() - daysAgo)
  const p = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())}`
}

async function json<T>(url: string, init?: RequestInit): Promise<T> {
  const res = await fetch(url, {
    ...init,
    headers: { 'Content-Type': 'application/json', ...(init?.headers ?? {}) },
  })
  if (!res.ok) throw new Error(`${url} → ${res.status}: ${await res.text()}`)
  return (await res.json()) as T
}

async function waitForHealth(url: string, timeoutMs: number, onTimeout: () => never): Promise<void> {
  const deadline = Date.now() + timeoutMs
  while (Date.now() < deadline) {
    try {
      const res = await fetch(url)
      if (res.ok) return
    } catch {
      /* 还没起来，继续等 */
    }
    await new Promise((r) => setTimeout(r, 250))
  }
  return onTimeout()
}

export function readState(): E2EState {
  return JSON.parse(readFileSync(STATE_FILE, 'utf8')) as E2EState
}
