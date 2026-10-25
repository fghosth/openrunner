#!/usr/bin/env python3
"""获取 Garmin 中国站（garmin.cn）的 OAuth1 凭据，用于 openrunner 国行同步。

为什么需要这一步：
  Garmin 中国站目前未开通官方 Personal API（令牌交换返回 526），
  但网页/App 会话体系可用。本脚本用 garth 登录一次，输出长期有效的
  OAuth1 token/secret；openrunner 服务端会用它在每次同步时自动换取
  24 小时的 OAuth2 token（无需你反复运行本脚本）。

用法：
  python3 -m pip install garth
  python3 scripts/garmin-cn-token.py you@example.com '你的密码'
  # 若账号开启了两步验证，脚本会提示输入验证码

输出示例（把两行值粘贴到 openrunner 的「平台连接 → 按域添加 → 中国站」）：
  oauth1_token = 1a2b3c...
  oauth1_secret = 4d5e6f...

安全提示：
  - 这等价于你的 Garmin 登录凭据，请勿分享；openrunner 会以 AES-256-GCM 加密存储。
  - 只在你自己信任的机器上运行。
"""
import sys


def main() -> int:
    if len(sys.argv) < 3:
        print(__doc__)
        return 1
    email, password = sys.argv[1], sys.argv[2]

    try:
        import garth
    except ImportError:
        print("缺少依赖：请先运行  python3 -m pip install garth", file=sys.stderr)
        return 1

    # 国行域；国行证书链不规范，需关闭 TLS 校验（garth 官方示例同样如此）
    garth.configure(domain="garmin.cn", ssl_verify=False)

    try:
        garth.login(email, password)
    except Exception as exc:  # noqa: BLE001 - 交给用户看清原因
        print(f"登录失败：{exc}", file=sys.stderr)
        print("提示：确认账号是中国站（garmin.cn）；若开了两步验证，交互终端下会提示输入验证码。", file=sys.stderr)
        return 2

    token = garth.client.oauth1_token
    if token is None:
        print("登录成功但未取得 OAuth1 token（可能需要先完成一次 App 登录）。", file=sys.stderr)
        return 3

    print()
    print("=== 复制下面两行到 openrunner（平台连接 → 中国站）===")
    print(f"oauth1_token = {token.oauth_token}")
    print(f"oauth1_secret = {token.oauth_token_secret}")
    print("=====================================================")
    print()
    print("有效性：长期（除非在 Garmin 侧撤销）；openrunner 会自动续期 OAuth2 token。")
    return 0


if __name__ == "__main__":
    sys.exit(main())
