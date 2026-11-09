import { useLocale } from './i18n'

/** 语言切换按钮（放进 header）。 */
export default function LocaleToggle() {
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
