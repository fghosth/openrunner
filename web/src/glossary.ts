/**
 * 术语表（专有名词 / 缩写的简略说明）。
 *
 * 设计要点：
 * - **一份数据源**：术语、别名、中英释义都在这，页面只写 `<GT text={...} />`，
 *   不各处塞 title 属性（那样既不能本地化也没法统一措辞）。
 * - **中英双解释义**：`zh` / `en` 与 i18n.ts 一样是数据，i18n-audit 已把本文件列入忽略。
 * - **匹配区分大小写**：否则英文正文里的 "if" 会被标成 IF。
 * - 释义要短：一句话讲清「是什么」，能一行写公式就写公式。口径以 docs/ALGORITHMS.md 为准。
 */

export type GlossaryEntry = {
  /** 稳定 id（也用于埋点/测试选择器） */
  key: string
  /** 规范写法，用于 tooltip 标题 */
  term: string
  /** 正文里出现这些 token 时触发（默认含 term 自身）；区分大小写、按词边界匹配 */
  aliases?: string[]
  /** 中文全称，跟在缩写后面 */
  fullZh?: string
  fullEn?: string
  zh: string
  en: string
}

export const glossary: GlossaryEntry[] = [
  // ---------- 负荷与压力 ----------
  {
    key: "np",
    term: "NP",
    fullZh: "标准化功率",
    fullEn: "Normalized Power",
    aliases: ["Normalized Power", "NS", "Normalized Speed", "标准化功率"],
    zh: "把波动的功率/配速折算成「等效恒定输出」：30 秒滑动平均后取四次方均值再开四次方。强度计算都以它为基准。",
    en: "Normalized Power: the steady output that costs the same as your variable effort — 30 s rolling average, then the 4th-power mean.",
  },
  {
    key: "tss",
    term: "TSS",
    fullZh: "训练压力分数",
    fullEn: "Training Stress Score",
    zh: "这节课累积了多少压力：100 × 时长(小时) × IF²。100 分 = 以阈值强度做满 1 小时。",
    en: "Training Stress Score: 100 × hours × IF². A score of 100 means one hour at threshold.",
  },
  {
    key: "rtss",
    term: "rTSS",
    fullZh: "跑步训练压力分数",
    fullEn: "running Training Stress Score",
    zh: "跑步版 TSS，用阈值配速代替临界功率：100 × 秒 × (NP ÷ 阈值配速)² ÷ 3600。",
    en: "Running TSS: uses threshold pace instead of critical power — 100 × secs × (NP ÷ threshold pace)² ÷ 3600.",
  },
  {
    key: "if",
    term: "IF",
    fullZh: "强度因子",
    fullEn: "Intensity Factor",
    aliases: ["Intensity Factor", "强度因子"],
    zh: "NP 相对阈值的比值。0.7 = 明显低于阈值，1.0 = 正好是阈值强度，>1.05 只能维持几分钟。",
    en: "NP relative to your threshold. 0.7 is easy, 1.0 is exactly threshold, >1.05 lasts only minutes.",
  },
  {
    key: "vi",
    term: "VI",
    fullZh: "变异指数",
    fullEn: "Variability Index",
    aliases: ["Variability Index", "变异指数"],
    zh: "NP ÷ 平均功率，衡量输出是否平稳。1.00 完全平稳；>1.05 说明忽快忽慢，同样体感压力更大。",
    en: "NP ÷ average power — how steady the effort was. 1.00 is perfectly even; above ~1.05 it was spiky.",
  },
  {
    key: "trimp",
    term: "TRIMP",
    fullZh: "训练冲动量",
    fullEn: "training impulse",
    aliases: ["training impulse"],
    zh: "按心率区间给每分钟加权（区间 1–5 权重 1/2/3/4/5）再求和。低于 64% 最大心率不计分。",
    en: "Minutes weighted by heart-rate zone (zones 1–5 score 1–5). Below 64% of max HR scores nothing.",
  },
  {
    key: "trimp100",
    term: "TRIMP100",
    fullZh: "归一化 TRIMP",
    fullEn: "normalized TRIMP",
    aliases: ["TRIMPv2"],
    zh: "把 TRIMP 缩放到「以 100 分为阈值强度 1 小时」的量纲，方便和 TSS 横向比较。",
    en: "TRIMP rescaled so that one hour at threshold equals 100, making it comparable with TSS.",
  },
  {
    key: "edwards",
    term: "Edwards",
    fullZh: "Edwards 训练量",
    fullEn: "Edwards training load",
    zh: "六区计时法：各区分钟数乘以区号后求和。比 TRIMP 粗糙，但不依赖个人阈值。",
    en: "Six-zone method: minutes per zone × zone number. Cruder than TRIMP but needs no personal threshold.",
  },
  {
    key: "powerload",
    term: "功率负荷",
    fullZh: undefined,
    fullEn: "Power load",
    aliases: ["Power load", "power load"],
    zh: "以功率口径（TSS）累计的日负荷，用于在趋势页切换 CTL/ATL/TSB 的计算基准。",
    en: "Daily load computed from power (TSS), used to switch the CTL/ATL/TSB basis on the trends page.",
  },

  // ---------- PMC ----------
  {
    key: "pmc",
    term: "PMC",
    fullZh: "体能-疲劳-形态图",
    fullEn: "Performance Management Chart",
    aliases: ["Performance Management Chart"],
    zh: "把每日训练负荷拆成体能、疲劳、形态三条曲线的长期趋势图。",
    en: "The chart that splits daily load into fitness, fatigue and form over time.",
  },
  {
    key: "ctl",
    term: "CTL",
    fullZh: "体能 / 慢性负荷",
    fullEn: "fitness / chronic load",
    aliases: ["Chronic Training Load", "LTS", "体能"],
    zh: "日负荷的 42 天指数加权平均（时间常数 42 天）。它涨得慢，代表你长期练出来的能力。",
    en: "Daily load exponentially averaged over 42 days. Rises slowly — the fitness you have built.",
  },
  {
    key: "atl",
    term: "ATL",
    fullZh: "疲劳 / 急性负荷",
    fullEn: "fatigue / acute load",
    aliases: ["Acute Training Load", "STS", "疲劳"],
    zh: "日负荷的 7 天指数加权平均。反应快、掉得也快，代表最近这几天的累积疲劳。",
    en: "Daily load exponentially averaged over 7 days. Reacts fast — the fatigue of the last few days.",
  },
  {
    key: "tsb",
    term: "TSB",
    fullZh: "形态 / 训练压力平衡",
    fullEn: "form / training stress balance",
    aliases: ["Training Stress Balance", "SB", "形态", "状态"],
    zh: "CTL − ATL。正值 = 恢复充分（比赛前想要 +15~+25），负值 = 带着疲劳（−20 左右是常见 overload 区）。",
    en: "CTL − ATL. Positive = fresh (race-ready +15 to +25); negative = fatigued (around −20 is a typical overload zone).",
  },
  {
    key: "acwr",
    term: "ACWR",
    fullZh: "急慢性负荷比",
    fullEn: "acute:chronic workload ratio",
    aliases: ["acute:chronic workload ratio"],
    zh: "ATL ÷ CTL。本项目**不采用**：比值在 CTL 很低时会爆炸，且其「安全区间」证据已被后续研究推翻。",
    en: "ATL ÷ CTL. Not used here: it explodes when CTL is low, and its 'safe zone' evidence has been challenged.",
  },
  {
    key: "seed",
    term: "播种",
    fullZh: undefined,
    fullEn: "seeding",
    aliases: ["seeding"],
    zh: "CTL/ATL 是指数平均，需要一个起点值。用赛季首日的负荷作为初值向前递推，避免赛季头几天被 0 拖低。",
    en: "CTL/ATL need a starting value. We seed them with the season's first-day load instead of starting from zero.",
  },
  {
    key: "banister",
    term: "Banister",
    fullZh: "Banister 双因子模型",
    fullEn: "Banister fitness–fatigue model",
    zh: "用回归拟合你自己的时间常数，把负荷映射成「表现」，比固定 42/7 天的 PMC 更个人化。",
    en: "Fits your own time constants by regression, mapping load to performance — more personal than fixed 42/7-day PMC.",
  },

  // ---------- 阈值与容量 ----------
  {
    key: "cp",
    term: "CP",
    fullZh: "临界功率",
    fullEn: "critical power",
    aliases: ["critical power", "临界功率"],
    zh: "理论上可以无限维持的功率上限，由多次力竭时长/功率拟合出来（三参数模型）。",
    en: "The power you could in theory hold forever, fitted from several all-out efforts (3-parameter model).",
  },
  {
    key: "ftp",
    term: "FTP",
    fullZh: "功能阈值功率",
    fullEn: "functional threshold power",
    aliases: ["functional threshold power"],
    zh: "约等于 1 小时最大平均功率的实用近似值。本项目以 CP 为准，FTP 只作参考。",
    en: "A practical stand-in for your best one-hour power. This project uses CP as the authority; FTP is a reference.",
  },
  {
    key: "wprime",
    term: "W′",
    fullZh: "临界功率以上储备",
    fullEn: "work capacity above critical power",
    aliases: ["W'", "work capacity above CP"],
    zh: "超过 CP 之后还能输出的总功（kJ）。用完就必须降到 CP 以下，否则无法维持。",
    en: "How much work (kJ) you can do above CP before you must drop below it.",
  },
  {
    key: "wbal",
    term: "W′bal",
    fullZh: "临界功率以上储备余量",
    fullEn: "remaining work above critical power",
    aliases: ["W'bal", "DBal"],
    zh: "W′ 的实时余量曲线：高于 CP 线性扣减，低于 CP 按指数回充（时间常数 τ）。",
    en: "Live remaining W′: drains linearly above CP, refills exponentially below it with time constant τ.",
  },
  {
    key: "cs",
    term: "CS",
    fullZh: "临界速度",
    fullEn: "critical speed",
    aliases: ["critical speed", "临界速度"],
    zh: "跑步版的临界功率：理论上可无限维持的速度，由多次力竭距离/时间拟合。",
    en: "The running equivalent of CP — the speed you could in theory hold forever, fitted from all-out efforts.",
  },
  {
    key: "dprime",
    term: "D′",
    fullZh: "临界速度以上距离储备",
    fullEn: "distance capacity above critical speed",
    aliases: ["D'", "distance capacity above CS"],
    zh: "超过 CS 之后还能多跑的距离（米），与 W′ 是同一个概念的速度版。",
    en: "Extra distance (m) you can cover above CS — the speed analogue of W′.",
  },
  {
    key: "cv",
    term: "%CV",
    fullZh: "相对临界速度的百分比",
    fullEn: "percentage of critical speed",
    zh: "以临界速度为 100% 划分配速区间，例如恢复区约 65–75% CV。",
    en: "Pace zones expressed as a share of critical speed, e.g. recovery is roughly 65–75% CV.",
  },
  {
    key: "thresholdpace",
    term: "阈值配速",
    fullZh: undefined,
    fullEn: "threshold pace",
    aliases: ["threshold pace"],
    zh: "乳酸开始快速堆积的那个配速，是 rTSS 与配速区间的基准。改了它记得重算历史指标。",
    en: "The pace where lactate starts to accumulate — the basis for rTSS and pace zones. Recompute history after changing it.",
  },

  // ---------- 预测 ----------
  {
    key: "riegel",
    term: "Riegel",
    fullZh: "Riegel 成绩外推",
    fullEn: "Riegel extrapolation",
    zh: "用已知成绩外推其他距离：T₂ = T₁ × (D₂ ÷ D₁)^α。外推越远误差越大。",
    en: "Projects other distances from a known result: T₂ = T₁ × (D₂ ÷ D₁)^α. Error grows with distance from the known race.",
  },
  {
    key: "alpha",
    term: "α",
    fullZh: "Riegel 指数",
    fullEn: "Riegel exponent",
    aliases: ["alpha"],
    zh: "距离拉长时掉速的快慢。α = 1 表示配速不变，>1 表示距离越远掉得越多；个人值由你的比赛成绩拟合。",
    en: "How much you slow down as distance grows. 1 = even pace, >1 = bigger fade; fitted from your own results.",
  },
  {
    key: "tanda",
    term: "Tanda",
    fullZh: "Tanda 周跑量模型",
    fullEn: "Tanda weekly-volume model",
    zh: "只用近 4 周周跑量预测半马/全马成绩的经验公式，不需要心率或阈值数据。",
    en: "Predicts half/full marathon from your recent weekly volume alone — no HR or threshold data needed.",
  },
  {
    key: "vdot",
    term: "VDOT",
    fullZh: "跑力值",
    fullEn: "VDOT running ability",
    zh: "Daniels 把比赛成绩折算成最大摄氧量等效值，再据此给出各课型的训练配速表。",
    en: "Daniels' conversion of a race time into a VO₂max-equivalent score, which then yields training paces.",
  },
  {
    key: "slowestcredible",
    term: "最慢可信值",
    fullZh: undefined,
    fullEn: "slowest credible time",
    aliases: ["slowest credible"],
    zh: "预测给出的保守一档：在训练量与已知成绩都成立的前提下，你「最慢也应该能做到」的成绩。",
    en: "The conservative end of a projection — the slowest time your training still credibly supports.",
  },

  // ---------- 心率 ----------
  {
    key: "lthr",
    term: "LTHR",
    fullZh: "乳酸阈值心率",
    fullEn: "lactate threshold heart rate",
    aliases: ["lactate threshold heart rate"],
    zh: "阈值强度时的心率。心率区间与 TRIMP 都以它为基准，改了要重算历史指标。",
    en: "Heart rate at threshold effort — the anchor for HR zones and TRIMP. Recompute history after changing it.",
  },
  {
    key: "maxhr",
    term: "MaxHR",
    fullZh: "最大心率",
    fullEn: "maximum heart rate",
    aliases: ["maximum heart rate", "最大心率", "观测最大心率"],
    zh: "本项目用「观测值」：历史活动里真实达到过的最高心率，比 220−年龄 的公式可靠。",
    en: "We use the observed value — the highest HR you have actually recorded — rather than a 220−age formula.",
  },
  {
    key: "resthr",
    term: "RestHR",
    fullZh: "静息心率",
    fullEn: "resting heart rate",
    aliases: ["resting heart rate", "静息心率"],
    zh: "清晨未活动时的最低心率，用于心率储备（Karvonen）区间计算。",
    en: "Your lowest morning HR; used with the heart-rate-reserve (Karvonen) zone method.",
  },
  {
    key: "hrr",
    term: "%HRR",
    fullZh: "心率储备百分比",
    fullEn: "heart-rate reserve percentage",
    aliases: ["heart-rate reserve"],
    zh: "（目标心率 − 静息）÷（最大 − 静息）。比直接用最大心率百分比更贴合个体差异。",
    en: "(target − resting) ÷ (max − resting). Fits individuals better than a plain % of max HR.",
  },
  {
    key: "karvonen",
    term: "Karvonen",
    fullZh: "Karvonen 心率储备法",
    fullEn: "Karvonen heart-rate-reserve method",
    zh: "1957 年提出的区间划分法：用最大与静息之间的那段储备按比例切分。",
    en: "The 1957 method that slices the span between resting and max HR by percentage.",
  },

  // ---------- 单课分析 ----------
  {
    key: "pahr",
    term: "Pa:Hr",
    fullZh: "配速-心率解耦",
    fullEn: "pace-to-heart-rate decoupling",
    aliases: ["decoupling", "解耦"],
    zh: "比较前后半程「每单位心率跑出多少配速」。偏离 >5% 说明后半程心脏更费力，是有氧耐力不足的信号。",
    en: "Compares pace-per-heart-beat across the two halves. Above ~5% means the second half cost more cardiovascularly.",
  },
  {
    key: "ef",
    term: "EF",
    fullZh: "跑步效率",
    fullEn: "efficiency factor",
    aliases: ["efficiency factor", "跑步效率"],
    zh: "每单位心率的配速（配速 ÷ 心率）。同配速下 EF 越高越省力，长期看趋势比看单次数值有用。",
    en: "Pace per heart beat (pace ÷ HR). Higher is cheaper; the trend matters more than any single number.",
  },
  {
    key: "drift",
    term: "Drift",
    fullZh: "心率漂移",
    fullEn: "cardiac drift",
    aliases: ["cardiac drift", "心率漂移"],
    zh: "配速不变时心率随时间缓慢上升。5% 以内正常，偏大常与脱水、高温或有氧基础不足有关。",
    en: "HR creeping up at constant pace. Under ~5% is normal; more often reflects heat, dehydration or a thin aerobic base.",
  },
  {
    key: "gap",
    term: "GAP",
    fullZh: "坡度修正配速",
    fullEn: "grade-adjusted pace",
    aliases: ["NGP", "grade-adjusted"],
    zh: "把爬坡/下坡的速度折算成平路等效值。本项目单课分析用原始速度——平路影响可忽略，山区跑会有偏差。",
    en: "Slope-adjusted equivalent pace. Session analysis here uses raw speed — negligible on flat routes, biased in the hills.",
  },

  // ---------- 强度分布 ----------
  {
    key: "zones",
    term: "Z1–Z7",
    fullZh: "强度分区",
    fullEn: "intensity zones",
    aliases: ["Z1", "Z2", "Z3", "Z4", "Z5", "Z6", "Z7"],
    zh: "把训练按强度切成的档位。心率用五/七区，功率用 Coggan 七区（以 CP 为基准），配速用 %CV 六区。",
    en: "Bands of effort. HR uses five/seven zones, power the seven Coggan zones (relative to CP), pace six %CV zones.",
  },
  {
    key: "s10",
    term: "S10–S13",
    fullZh: "强度分布分桶",
    fullEn: "intensity distribution buckets",
    aliases: ["S10", "S11", "S12", "S13"],
    zh: "把训练时长按强度切成四个桶（S10 最低 → S13 最高），用来核对是否接近 80/20 的极化分布。",
    en: "Training time split into four intensity buckets (S10 lowest → S13 highest), used to check a polarised 80/20 spread.",
  },

  {
    key: "pb",
    term: "个人最佳",
    fullEn: "personal best",
    aliases: ["PB", "Personal best", "personal best"],
    zh: "各目标距离（1K/5K/10K/半马/全马）里跑过的最快分段，从速度流里扫出来，不是整场比赛的成绩。",
    en: "The fastest split you have ever run for each target distance, scanned from the speed stream — not a whole-race result.",
  },
  {
    key: "weekcheck",
    term: "下周计划校验",
    fullEn: "next-week plan check",
    aliases: ["Check next week"],
    zh: "把下周预排的课表和近期实际训练量对比，按公开研究的安全增量规则给出提示；校验通过才会写入日历。",
    en: "Compares next week's planned runs against recent actual load using published safe-increment rules; only passing plans get written.",
  },
  {
    key: "weeklyvolume",
    term: "周跑量",
    fullEn: "weekly volume",
    aliases: ["weekly volume"],
    zh: "一周内**已完成**的距离总和（计划课不算）。只统计完整的周，休息周也算一周——这是预测与增量规则的基准。",
    en: "Distance actually completed in a week (planned runs excluded). Only whole weeks count, rest weeks included.",
  },
  {
    key: "volume",
    term: "周增量",
    fullEn: "week-over-week increase",
    aliases: ["Volume"],
    zh: "下周计划跑量相对近期基准的变化幅度。研究里普遍建议一周别加超过 ~10%。",
    en: "How much next week's volume changes against your recent baseline. Literature suggests staying under ~10% per week.",
  },
  {
    key: "deload",
    term: "减量节奏",
    fullEn: "deload rhythm",
    aliases: ["Deload rhythm", "减量"],
    zh: "连续加量几周后安排一个低量周。长期只加不减会让疲劳累积，受伤风险上升。",
    en: "A low-volume week after several building weeks — without it fatigue compounds and injury risk rises.",
  },
  {
    key: "spike",
    term: "单次 +10%",
    fullEn: "single-run spike limit",
    aliases: ["Single-run spike", "+10%"],
    zh: "单日最长课不应比近期最长课高出约 10% 以上——最常被引用的单课增幅上限。",
    en: "Your longest single session should not exceed the recent longest by more than ~10%.",
  },
  {
    key: "polarised",
    term: "80/20",
    fullZh: "极化强度分布",
    fullEn: "polarised intensity distribution",
    aliases: ["极化"],
    zh: "约八成训练量落在低强度、两成落在高强度。S10–S13 用来核对实际分布是否接近它。",
    en: "Roughly 80% of training volume low-intensity, 20% high. S10–S13 shows how close your actual spread is.",
  },
  {
    key: "dailyload",
    term: "训练负荷",
    fullEn: "training load",
    aliases: ["training load", "日负荷", "Daily load"],
    zh: "把每节课的压力分（TRIMP 或 rTSS）按天累加。它是 CTL/ATL/TSB 的输入。",
    en: "Daily sum of session stress scores (TRIMP or rTSS) — the input to CTL/ATL/TSB.",
  },
  // ---------- 其它 ----------
  {
    key: "planned",
    term: "计划课",
    fullZh: undefined,
    fullEn: "planned workout",
    aliases: ["planned workout"],
    zh: "日历里预排但还没跑的训练。只用于「下周计划校验」，不计入 PB、轨迹墙与实际统计。",
    en: "A workout scheduled but not yet done. Used for week-check only; excluded from PBs, the track wall and actual stats.",
  },
]

// ---------------------------------------------------------------- 匹配

/** token → entry。别名与规范写法都指向同一条。 */
const byToken = new Map<string, GlossaryEntry>()
for (const e of glossary) {
  for (const tok of [e.term, ...(e.aliases ?? [])]) {
    if (!byToken.has(tok)) byToken.set(tok, e)
  }
}

export function glossaryByToken(token: string): GlossaryEntry | undefined {
  return byToken.get(token)
}

/**
 * 按 token 长度降序拼一条正则：长的优先，避免 "W′bal" 被 "W′" 抢先匹配。
 * 词边界只对「字母数字结尾」的 token 加，否则 `W′`（以非词字符结尾）会匹配不上。
 */
const tokenPattern = (() => {
  const tokens = [...byToken.keys()].sort((a, b) => b.length - a.length)
  const esc = (s: string) => s.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")
  const parts = tokens.map((t) => {
    const wordChar = /[\w\u4e00-\u9fff]/.test(t[0]) && /[\w\u4e00-\u9fff]/.test(t[t.length - 1])
    return wordChar ? `(?<![\\w])${esc(t)}(?![\\w])` : esc(t)
  })
  return new RegExp(parts.join("|"), "g")
})()

export type TextPiece =
  | { kind: "text"; text: string }
  | { kind: "term"; text: string; entry: GlossaryEntry }

/** 把一段文本切成「普通片段」与「术语片段」，供 <GT> 渲染。 */
export function findTerms(text: string): TextPiece[] {
  if (!text) return []
  tokenPattern.lastIndex = 0
  const out: TextPiece[] = []
  let last = 0
  for (const m of text.matchAll(tokenPattern)) {
    const idx = m.index ?? 0
    if (idx > last) out.push({ kind: "text", text: text.slice(last, idx) })
    const tok = m[0]
    const entry = byToken.get(tok)
    if (entry) out.push({ kind: "term", text: tok, entry })
    else out.push({ kind: "text", text: tok })
    last = idx + tok.length
  }
  if (last < text.length) out.push({ kind: "text", text: text.slice(last) })
  return out
}
