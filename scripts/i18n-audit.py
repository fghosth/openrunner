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

ROOT = pathlib.Path(__file__).resolve().parent.parent   # 仓库根
UI = ROOT / "web"

# 只列「界面文案应该走 i18n」的文件。两个数据文件故意排除：
#   src/i18n.ts        —— 中英字典本身
#   src/glossary.ts    —— 术语表，中英双解释义是数据不是硬编码

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


def audit_hardcoded() -> int:
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


def main() -> int:
    rc = audit_hardcoded()
    # 第二段：服务端下发的 i18n key 必须在字典里（拼错只会静默回落中文，不报错）
    if not check_server_keys(ROOT, UI):
        rc = 1
    return rc


# ---------------------------------------------------------------- 服务端下发 key 的覆盖检查
#
# 服务端会把「稳定 code + 中文原文」一起下发（平台表单文案、预测标签、判定原因）。
# 前端按 code 取本地文案、取不到回落原文——所以 code 拼错/漏翻译**不会报错**，
# 只会静默显示中文（踩过：i18n 里叫 raceHalf、服务端发 race.half）。
# 这道检查把服务端源码里所有 i18n key 字面量抓出来，逐个对字典。

GO_KEY_PATTERNS = [
    r'"(platform\.[a-zA-Z0-9.]+)"',
    r'"(race\.[a-zA-Z0-9]+)"',
    r'"(note\.[a-zA-Z0-9]+)"',
]
# code → i18n key 的映射表在前端：从这几个文件里抓 'x': 'yKey' 形式
CODE_MAP_FILES = ["src/analysis-text.ts", "src/pages/PerformanceForecast.tsx"]


def server_keys(repo: pathlib.Path) -> set[str]:
    keys: set[str] = set()
    for root in (ROOT / "server", ROOT / "core"):
        for f in root.rglob("*.go"):
            if f.name.endswith("_test.go"):
                continue
            text = f.read_text(errors="ignore")
            for pat in GO_KEY_PATTERNS:
                keys.update(re.findall(pat, text))
    return keys


def ui_keys(ui: pathlib.Path) -> set[str]:
    text = (ui / "src/i18n.ts").read_text()
    keys = set(re.findall(r"^\s{4}'([^']+)':", text, re.M))
    keys.update(re.findall(r"^\s{4}([A-Za-z][A-Za-z0-9]*):", text, re.M))
    return keys


def check_server_keys(repo: pathlib.Path, ui: pathlib.Path) -> bool:
    if not ui.exists():
        print("（跳过服务端 key 检查：找不到 web/）")
        return True
    want = server_keys(repo)
    have = ui_keys(ui)
    missing = sorted(k for k in want if k not in have)
    if missing:
        print(f"✗ 服务端下发的 i18n key 有 {len(missing)} 个不在字典里（英文界面会回落成中文）：")
        for k in missing:
            print(f"    {k}")
        return False
    print(f"✓ 服务端下发的 {len(want)} 个 i18n key 都在字典里")
    return True


if __name__ == "__main__":
    sys.exit(main())

