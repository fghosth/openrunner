#!/usr/bin/env python3
"""把含术语的 i18n 文案在 JSX children 位置包成 <GT text={t('k')} />。

只在 children 位置替换：前一个字符必须是 `>` 或空白，绝不能是 `=`，
否则会把 aria-label={t('x')} 这种属性值包成 JSX（编译不过 / 属性里塞组件）。
"""
import re
import pathlib
import sys

UI = pathlib.Path(__file__).resolve().parents[2] / "openrunner-ui"
if not UI.exists():  # 允许在 openrunner-ui 目录里直接跑
    UI = pathlib.Path.cwd()

TERMS = [
    "TSS", "rTSS", "CTL", "ATL", "TSB", "ACWR", "NP", "IF", "VI",
    "W′bal", "W'bal", "W′", "W'", "CP", "CS", "D′", "D'", "VDOT",
    "Riegel", "Tanda", "Pa:Hr", "EF", "Drift", "LTHR", "MaxHR", "RestHR",
    "%HRR", "%CV", "Karvonen", "Banister", "Edwards", "TRIMP", "PMC", "FTP",
    "GAP", "NGP", "S10", "S11", "S12", "S13", "α",
    "计划课", "阈值配速", "播种", "最慢可信值", "解耦", "漂移", "跑步效率",
    "体能", "疲劳", "形态", "功率负荷",
    "个人最佳", "PB", "下周计划校验", "周跑量", "周增量", "减量节奏", "单次 +10%", "80/20", "训练负荷", "日负荷",
]


def term_keys() -> list[str]:
    src = (UI / "src/i18n.ts").read_text()
    # 字典声明是 `const zh = {` / `const en: Record<TKey, string> = {`；
    # 只取 zh 段，否则同名 key 会被英文值覆盖（踩过：按英文值找术语，漏掉「个人最佳」）
    zh = re.split(r"^const en\b", src, maxsplit=1, flags=re.M)[0]
    vals = dict(re.findall(r"^\s{4}'?([A-Za-z0-9._]+)'?:\s*'([^']*)'", zh, re.M))
    vals.update(dict(re.findall(r'^\s{4}"?([A-Za-z0-9._]+)"?:\s*"([^"]*)"', zh, re.M)))
    out = []
    for k, v in vals.items():
        # aria-* 是属性、tip*/​*Tip 已有富文案说明，都不需要再挂术语
        if "aria" in k.lower() or k.startswith("tip") or "Tip" in k:
            continue
        if any(x in v for x in TERMS):
            out.append(k)
    return sorted(set(out))


def wrap(text: str, keys: list[str]) -> tuple[str, int]:
    pat = re.compile(r"t\('(" + "|".join(map(re.escape, keys)) + r")'")
    out = []
    i = 0
    n = 0
    while True:
        m = pat.search(text, i)
        if not m:
            out.append(text[i:])
            break
        tpos = m.start()                       # 't' of t(
        brace = tpos - 1                       # the '{' before it
        if brace < 0 or text[brace] != "{":
            out.append(text[i:m.end()])
            i = m.end()
            continue
        prev = text[brace - 1] if brace else ""
        if prev not in "> \t\n{":              # 排除 label={t(...)} 这类属性
            out.append(text[i:m.end()])
            i = m.end()
            continue
        depth = 0
        j = tpos + 1                           # '(' after t
        while j < len(text):
            c = text[j]
            if c == "(":
                depth += 1
            elif c == ")":
                depth -= 1
                if depth == 0:
                    break
            j += 1
        if j + 1 >= len(text) or text[j + 1] != "}":
            out.append(text[i:m.end()])
            i = m.end()
            continue
        full = text[brace : j + 2]             # {t('k', ...)}
        out.append(text[i:brace])
        out.append("<GT text={" + full[1:-1] + "} />")
        n += 1
        i = j + 2
    return "".join(out), n


def main() -> int:
    keys = term_keys()
    only = sys.argv[1:]
    files = sorted((UI / "src/pages").glob("*.tsx"))
    if only:
        files = [f for f in files if f.name in only]
    total = 0
    for f in files:
        s = f.read_text()
        new, n = wrap(s, keys)
        if not n:
            continue
        lines = new.split("\n")
        last = max(idx for idx, l in enumerate(lines) if l.startswith("import "))
        if "from '../glossary-term'" not in new:
            lines.insert(last + 1, "import { GT } from '../glossary-term'")
            new = "\n".join(lines)
        f.write_text(new)
        total += n
        print(f"  {f.name}: {n}")
    print("总计", total)
    return 0


if __name__ == "__main__":
    sys.exit(main())
