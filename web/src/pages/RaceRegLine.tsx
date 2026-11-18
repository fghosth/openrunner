import type { RaceEvent } from '../api'
import { useLocale } from '../i18n'
import {
  fmtPrice,
  fmtRegWhen,
  nowInBeijing,
  regClosed,
  regNotOpenYet,
} from '../race-ui'
import RaceCopyButton from './RaceCopyButton'

/** 报名那一行，横滚条与时间轴行共用（之前两处各写一份，形状还漂了）。
 *
 * 田协赛历**不带报名窗口和价格**，这层来自最酷，覆盖率天生不满（大田协赛常在
 * 数字心动/马拉马拉上架）。四条路都要有归宿：
 *   正在报名 → 截止 + 起价 + 直达表单；
 *   还没开放 → 「报名开始 X」，**不给表单**（那页还没开）；
 *   已经截止 → 「报名已截止」，**但保留复制入口** —— 最酷截止不等于别家不能报；
 *   没配上   → 复制入口。
 * 共同红线：不显示猜的日期，不显示 ¥0。
 */
export default function RaceRegLine({ race, divider = true }: { race: RaceEvent; divider?: boolean }) {
  const { t } = useLocale()
  const now = nowInBeijing()
  const reg = race.registration
  const cls = `flex flex-wrap items-center gap-1.5 text-xs ${
    divider ? 'mt-1.5 border-t border-border/60 pt-1.5' : ''
  }`

  if (!reg) return <div className={cls}><RaceCopyButton race={race} /></div>

  const closed = regClosed(reg.regEnd, now)
  const notYet = regNotOpenYet(reg, now)

  return (
    <div className={cls}>
      {closed && (
        <span className="whitespace-nowrap rounded bg-muted px-1.5 py-0.5 text-muted-foreground">
          {t('racesRegClosed')}
        </span>
      )}
      {!closed && notYet && (
        <span className="whitespace-nowrap rounded bg-secondary/15 px-1.5 py-0.5 font-medium text-secondary">
          {t('racesRegStarts', { when: fmtRegWhen(reg.regStart) })}
        </span>
      )}
      {!closed && !notYet && reg.regEnd && (
        <span className="whitespace-nowrap rounded bg-accent/15 px-1.5 py-0.5 font-medium text-accent">
          {t('racesRegDeadline', { when: fmtRegWhen(reg.regEnd) })}
        </span>
      )}
      {/* 价格和报名链接只在「现在真的能报」时给 —— 在已关闭或还没开的窗口旁边
          报价格和「去报名」，是在告诉用户一件他做不到的事 */}
      {!closed && !notYet && reg.minPrice > 0 && (
        <span className="whitespace-nowrap font-mono text-foreground">
          ¥{fmtPrice(reg.minPrice)} {t('racesPriceFrom')}
        </span>
      )}
      {!closed && !notYet && reg.regUrl && (
        <a
          href={reg.regUrl}
          target="_blank"
          rel="noreferrer noopener"
          className="whitespace-nowrap text-secondary hover:underline"
        >
          {t('racesGoRegister')}
        </a>
      )}
      {/* 最酷截止了不代表别家不能报，复制入口要留着 */}
      {closed && <RaceCopyButton race={race} />}
    </div>
  )
}
