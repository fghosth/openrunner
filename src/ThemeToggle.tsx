import { useEffect, useState } from 'react'
import { useLocale } from './i18n'

const STORAGE_KEY = 'openrunner.theme'
type Theme = 'dark' | 'light'

function readTheme(): Theme {
  const saved = localStorage.getItem(STORAGE_KEY)
  if (saved === 'light' || saved === 'dark') return saved
  // 未设置过：跟随系统
  return window.matchMedia('(prefers-color-scheme: light)').matches ? 'light' : 'dark'
}

/** 暗/亮主题切换（running_page 的 useTheme 对应物）。
 *
 * 主题落在 `document.documentElement.dataset.theme`，index.css 里用
 * `html[data-theme="light"]` 覆盖 Tailwind 的颜色变量。
 */
export default function ThemeToggle() {
  const { t } = useLocale()
  const [theme, setTheme] = useState<Theme>(readTheme)

  useEffect(() => {
    document.documentElement.dataset.theme = theme
    localStorage.setItem(STORAGE_KEY, theme)
  }, [theme])

  const next: Theme = theme === 'dark' ? 'light' : 'dark'

  return (
    <button
      onClick={() => setTheme(next)}
      title={next === 'light' ? t('themeToLight') : t('themeToDark')}
      aria-label={next === 'light' ? t('themeToLight') : t('themeToDark')}
      className="cursor-pointer rounded border border-border px-2 py-1.5 text-sm text-muted-foreground hover:text-foreground"
    >
      {theme === 'dark' ? t('themeDarkLabel') : t('themeLightLabel')}
    </button>
  )
}
