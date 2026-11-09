/** 单课分析「不适用」原因的本地化（服务端下发 code + 中文原文）。 */

const REASON_KEYS: Record<string, string> = {
  tooShort: 'analysisTooShort',
  badInterval: 'analysisBadInterval',
  noStream: 'analysisNoStream',
  tooFewSamples: 'analysisTooFewSamples',
}

/** 把服务端的 reasonCode 翻成本地文案；没有 code 或未翻译则回落服务端原文。 */
export function analysisReason(
  code: string | undefined,
  fallback: string | undefined,
  serverText: (key?: string, fallback?: string) => string,
): string {
  const key = code ? REASON_KEYS[code] : undefined
  return serverText(key, fallback)
}
