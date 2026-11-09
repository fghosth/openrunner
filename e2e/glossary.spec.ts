/**
 * 术语表自身的测试（不需要浏览器；Playwright 直接 import TS 模块）。
 *
 * 为什么单独一份：匹配器是纯正则，页面上「看起来没问题」不代表边界情况对。
 * 真实风险是**静默失效**——术语没被标记，用户只是看不到说明，没人会报 bug。
 */
import { expect, test } from '@playwright/test'
import { findTerms, glossary, glossaryByToken } from '../src/glossary'

const termKeys = (text: string) =>
  findTerms(text)
    .filter((p) => p.kind === "term")
    .map((p) => (p.kind === "term" ? p.entry.key : ""))

test('术语表数据完整：key 唯一、中英释义都在、别名不撞车', () => {
  const keys = glossary.map((e) => e.key)
  expect(new Set(keys).size).toBe(keys.length)
  for (const e of glossary) {
    expect(e.zh.length, `${e.key} 缺中文释义`).toBeGreaterThan(8)
    expect(e.en.length, `${e.key} 缺英文释义`).toBeGreaterThan(8)
    expect(e.term.length).toBeGreaterThan(0)
  }
  // 同一个 token 不能指向两条术语（先到先得会静默吞掉后者）
  const seen = new Map<string, string>()
  for (const e of glossary) {
    for (const tok of [e.term, ...(e.aliases ?? [])]) {
      const prev = seen.get(tok)
      // 匹配表是 first-wins：跨条目撞车会静默吞掉后者，必须抓红
      if (prev !== undefined) expect(prev, `token "${tok}" 同时属于 ${prev} 和 ${e.key}`).toBe(e.key)
      seen.set(tok, e.key)
    }
  }
})

test('长词优先：W′bal 不被 W′ 抢先', () => {
  expect(termKeys('W′bal 曲线')).toEqual(["wbal"])
  expect(termKeys("W'bal 曲线")).toEqual(["wbal"])
  expect(termKeys('W′ 储备')).toEqual(["wprime"])
})

test('词边界：不能从单词中间切出术语', () => {
  expect(termKeys('NPP 不是 NP')).toEqual(["np"]) // 只有独立的 NP
  expect(termKeys('CPL 与 CP')).toEqual(["cp"])
  expect(termKeys('W′balance 里只有 W′')).toEqual(["wprime", "wprime"])
})

test('区分大小写：英文正文里的 if/ef 不算术语', () => {
  expect(termKeys('if your ef is low')).toEqual([])
  expect(termKeys('IF 与 EF')).toEqual(["if", "ef"])
})

test('中文术语与冒号形式都能匹配', () => {
  expect(termKeys('Pa:Hr 解耦')).toEqual(["pahr", "pahr"])  // 解耦 是 Pa:Hr 的别名
  expect(termKeys('下周计划校验通过')).toEqual(["weekcheck"])
  expect(termKeys('个人最佳 PB')).toEqual(["pb", "pb"])
  expect(termKeys('80/20 与 S10')).toEqual(["polarised", "s10"])
})

test('PMC 图例的内部名映射到 CTL/ATL/TSB', () => {
  expect(glossaryByToken('LTS')?.key).toBe('ctl')
  expect(glossaryByToken('STS')?.key).toBe('atl')
  expect(glossaryByToken('SB')?.key).toBe('tsb')
})

test('空串与无术语文本不炸', () => {
  expect(findTerms('')).toEqual([])
  expect(findTerms('普通文本')).toEqual([{ kind: 'text', text: '普通文本' }])
})
