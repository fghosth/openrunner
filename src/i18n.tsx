import { createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from 'react'

/** 轻量 i18n（对齐 running_page 的中英切换）。
 *
 * 只覆盖「导航 + 首页概览」这类首屏可见文案。设置页、活动详情等功能页当前仍为
 * 中文 —— 完整迁移会让本轮改动面过大，已在 DEVLOG 记为挂账项。
 */

export type Locale = 'zh' | 'en'

const STORAGE_KEY = 'openrunner.locale'

const dict = {
  zh: {
    // 导航
    navActivities: '活动',
    navTrends: '趋势',
    navMap: '地图',
    navTracksWall: '轨迹墙',
    navStats: '年度',
    logout: '登出',
    themeToLight: '切换到亮色',
    themeToDark: '切换到暗色',
    // 概览
    overviewTitle: '年总览',
    cumulative: '累计',
    times: '次',
    // 统计卡片
    distance: '距离',
    count: '次数',
    duration: '时间',
    elevation: '爬升',
    thisMonth: '本月',
    thisWeek: '本周',
    // 热力图
    trainingCalendar: '训练日历',
    daysWithActivity: '天有活动',
    total: '合计',
    less: '少',
    more: '多',
    // 个人最佳
    personalBest: '个人最佳',
    computing: '统计中…',
    pbNote: '在每条跑步活动内部找最快分段，因此一次 10K 跑的前半段也可能算作 5K 最佳。',
    // 足迹地图
    footprintMap: '足迹地图',
    provincesCovered: '个省级行政区',
    clearSelection: '清除选择',
    noGpsYet: '还没有带 GPS 的活动',
    noActivity: '暂无活动',
    // 月历
    calendar: '月历',
    daysThisMonth: '天有活动',
    noActivityThisMonth: '本月没有活动',
    minutes: '分钟',
    // 资料卡
    totalDistance: '累计距离',
    totalCount: '累计次数',
    totalTime: '累计时间',
    activeThisYear: '年活跃',
    gpsTracks: '条 GPS 轨迹',
    totalElevation: '累计爬升',
  },
  en: {
    navActivities: 'Activities',
    navTrends: 'Trends',
    navMap: 'Map',
    navTracksWall: 'Track Wall',
    navStats: 'Yearly',
    logout: 'Sign out',
    themeToLight: 'Switch to light',
    themeToDark: 'Switch to dark',
    overviewTitle: 'Year overview',
    cumulative: 'total',
    times: '',
    distance: 'Distance',
    count: 'Count',
    duration: 'Time',
    elevation: 'Elevation',
    thisMonth: 'This month',
    thisWeek: 'This week',
    trainingCalendar: 'Training calendar',
    daysWithActivity: 'days active',
    total: 'total',
    less: 'Less',
    more: 'More',
    personalBest: 'Personal bests',
    computing: 'Computing…',
    pbNote: 'Fastest segment inside each run — the first half of a 10K can be a 5K PB.',
    footprintMap: 'Footprint',
    provincesCovered: 'provinces',
    clearSelection: 'Clear',
    noGpsYet: 'No activities with GPS yet',
    noActivity: 'No activities',
    calendar: 'Calendar',
    daysThisMonth: 'days active',
    noActivityThisMonth: 'No activity this month',
    minutes: 'min',
    totalDistance: 'Total distance',
    totalCount: 'Total activities',
    totalTime: 'Total time',
    activeThisYear: 'active in',
    gpsTracks: 'GPS tracks',
    totalElevation: 'Total elevation',
  },
} as const

export type TKey = keyof (typeof dict)['zh']

type Ctx = { locale: Locale; setLocale: (l: Locale) => void; t: (k: TKey) => string }

const LocaleContext = createContext<Ctx>({
  locale: 'zh',
  setLocale: () => undefined,
  t: (k) => dict.zh[k],
})

function readLocale(): Locale {
  const v = localStorage.getItem(STORAGE_KEY)
  return v === 'en' ? 'en' : 'zh'
}

export function LocaleProvider({ children }: { children: ReactNode }) {
  const [locale, setLocaleState] = useState<Locale>(readLocale)

  const setLocale = useCallback((l: Locale) => {
    setLocaleState(l)
    localStorage.setItem(STORAGE_KEY, l)
  }, [])

  useEffect(() => {
    document.documentElement.lang = locale === 'en' ? 'en' : 'zh-CN'
  }, [locale])

  const value = useMemo(
    () => ({ locale, setLocale, t: (k: TKey) => dict[locale][k] ?? dict.zh[k] }),
    [locale, setLocale],
  )
  return <LocaleContext.Provider value={value}>{children}</LocaleContext.Provider>
}

export function useLocale(): Ctx {
  return useContext(LocaleContext)
}

/** 语言切换按钮（放进 header）。 */
export function LocaleToggle() {
  const { locale, setLocale } = useLocale()
  return (
    <button
      onClick={() => setLocale(locale === 'zh' ? 'en' : 'zh')}
      title={locale === 'zh' ? 'Switch to English' : '切换到中文'}
      className="cursor-pointer rounded border border-border px-2 py-1.5 text-sm text-muted-foreground hover:text-foreground"
    >
      {locale === 'zh' ? '中' : 'EN'}
    </button>
  )
}
