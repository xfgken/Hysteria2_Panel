#!/usr/bin/env bash
#
# 校验面板渲染出的 Clash Meta 订阅是否为合法 YAML，并展示实际缩进。
#
# 用法：bash check-sub-yaml.sh <订阅地址>

set -u

SUB="${1:?请提供订阅地址}"

curl -s "$SUB" > /tmp/sub.yaml

echo "===== 代理段原文（cat -A 显示真实缩进，\$ 表示行尾）====="
sed -n '/^proxies:/,/^proxy-groups:/p' /tmp/sub.yaml | cat -A

echo
echo "===== YAML 解析验证 ====="
if command -v python3 >/dev/null 2>&1; then
  python3 - <<'PY'
import sys
try:
    import yaml
except ImportError:
    print("（未安装 pyyaml，跳过解析，仅做缩进检查）")
    sys.exit(0)

try:
    d = yaml.safe_load(open('/tmp/sub.yaml'))
except Exception as e:
    print("✗ YAML 解析失败：", e)
    sys.exit(1)

print("✓ YAML 解析成功")
proxies = d.get('proxies') or []
print("  代理数量:", len(proxies))
for p in proxies:
    if isinstance(p, dict):
        print("  -", {k: v for k, v in p.items() if k != 'password'})
groups = d.get('proxy-groups') or []
print("  策略组:", [g.get('name') for g in groups if isinstance(g, dict)])
rules = d.get('rules') or []
print("  规则条数:", len(rules))
PY
else
  echo "（无 python3，跳过 YAML 解析验证）"
fi