#!/usr/bin/env python3
"""票 07 验收：功能页无残留中文硬编码的 grep 断言。

规则（对票面范围内的 UI 文件）：
  - 代码（去掉注释后）出现 CJK 即违规；
  - 行内含 `i18n-allow` 标记的除外（服务端英文原文的 zh 显示映射、
    票 02 判定模板、日期桶标签——各自注明理由）；
  - src/i18n.ts 本身是字典，不检查。
退出码非 0 = 有残留。用法：python3 scripts/i18n-audit.py
"""
import pathlib
import re
import sys

UI = pathlib.Path(__file__).resolve().parent.parent.parent / "openrunner-ui"

SCOPE = [
    "src/pages/Settings.tsx",
    "src/pages/HrZoneEditor.tsx",
    "src/pages/PaceZoneEditor.tsx",
    "src/pages/CsZoneEditor.tsx",
    "src/pages/ManualActivityForm.tsx",
    "src/pages/RaceResultsEditor.tsx",
    "src/pages/PlatformSettings.tsx",
    "src/pages/CoachPacesCard.tsx",
    "src/pages/CoachActivityAnalysis.tsx",
    "src/pages/CoachWeekPanel.tsx",
    "src/pages/MarathonPredict.tsx",
    "src/pages/ActivityDetail.tsx",
    "src/pages/Trends.tsx",
    "src/pages/PerformanceForecast.tsx",
    "src/pages/MapStats.tsx",
    "src/pages/SummaryPage.tsx",
    "src/pages/TracksWall.tsx",
]

CJK = re.compile(r"[一-鿿]")
COMMENT_LINE = re.compile(r"^\s*(//|\*|/\*)")


def violations(path: pathlib.Path):
    out = []
    text = path.read_text()
    # 先整体剥掉多行 JSX 注释 {/* ... */}，保留行号
    text = re.sub(r"\{/\*[\s\S]*?\*/\}", lambda m: "\n" * m.group(0).count("\n"), text)
    for i, line in enumerate(text.splitlines(), 1):
        if COMMENT_LINE.match(line) or "i18n-allow" in line:
            continue
        code = re.sub(r"//.*$", "", line)
        if CJK.search(code):
            out.append((i, line.strip()[:80]))
    return out


def main() -> int:
    bad = 0
    for rel in SCOPE:
        f = UI / rel
        if not f.exists():
            print(f"MISSING FILE: {rel}")
            bad += 1
            continue
        for ln, text in violations(f):
            print(f"{rel}:{ln}: {text}")
            bad += 1
    if bad:
        print(f"\n✗ {bad} 处残留中文硬编码")
        return 1
    print(f"✓ {len(SCOPE)} 个文件无残留中文硬编码")
    return 0


if __name__ == "__main__":
    sys.exit(main())
