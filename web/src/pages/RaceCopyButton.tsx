import { useState } from 'react'
import type { RaceEvent } from '../api'
import { useLocale } from '../i18n'

/** 「复制赛事名去官方渠道报名」。
 *
 * 最酷只代理一部分赛事，没配上的那批拿不到报名窗口和价格。
 * 与其什么都不给（会被读成「这比赛不能报了」），不如把「赛事名 + 日期」
 * 交给用户，让他粘到数字心动 / 马拉马拉 / 官方公众号里自己搜。
 * 剪贴板不可用时不谎报「已复制」。
 */
export default function RaceCopyButton({ race }: { race: RaceEvent }) {
  const { t } = useLocale()
  const [copied, setCopied] = useState(false)
  const text = `${race.name} ${race.date}`

  async function copy() {
    try {
      await navigator.clipboard.writeText(text)
      setCopied(true)
    } catch {
      setCopied(false)
    }
  }

  return (
    <button
      type="button"
      onClick={() => void copy()}
      title={text}
      className="cursor-pointer whitespace-nowrap text-muted-foreground hover:text-secondary"
    >
      {copied ? t('racesCopied') : t('racesCopyToRegister')}
    </button>
  )
}
