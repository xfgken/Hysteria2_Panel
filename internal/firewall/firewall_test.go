package firewall

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// makeTool 在临时目录里造一个假命令：把自己的参数追加到 <dir>/<name>.args，
// 并按 script 指定的行为输出内容。这样能在不碰真实防火墙的前提下验证逻辑。
func makeTool(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	script := "#!/bin/sh\necho \"$@\" >> " + filepath.Join(dir, name+".args") + "\n" + body + "\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func argsOf(t *testing.T, dir, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, name+".args"))
	if err != nil {
		return ""
	}
	return string(b)
}

// TestEnsureUDPWithUfw 确认检测到 ufw 时用 ufw 放行，且单个端口写法正确。
func TestEnsureUDPWithUfw(t *testing.T) {
	dir := t.TempDir()
	makeTool(t, dir, "ufw", "exit 0")
	t.Setenv("PATH", dir)

	res := EnsureUDP(8443, 0)
	if res.Firewall != "ufw" || !res.Opened {
		t.Fatalf("结果不对: %+v", res)
	}
	if got := argsOf(t, dir, "ufw"); !strings.Contains(got, "allow 8443/udp") {
		t.Errorf("ufw 参数不对: %q", got)
	}
}

// TestEnsureUDPRangeUsesColonForUfw 确认端口跳跃（范围）在 ufw 里用冒号写法。
func TestEnsureUDPRangeUsesColonForUfw(t *testing.T) {
	dir := t.TempDir()
	makeTool(t, dir, "ufw", "exit 0")
	t.Setenv("PATH", dir)

	res := EnsureUDP(20000, 50000)
	if !res.Opened {
		t.Fatalf("应放行整段范围: %+v", res)
	}
	if got := argsOf(t, dir, "ufw"); !strings.Contains(got, "allow 20000:50000/udp") {
		t.Errorf("ufw 范围写法不对: %q", got)
	}
}

// TestEnsureUDPWithFirewalld 确认 firewalld 走 --permanent + --reload。
func TestEnsureUDPWithFirewalld(t *testing.T) {
	dir := t.TempDir()
	makeTool(t, dir, "firewall-cmd", "exit 0")
	t.Setenv("PATH", dir)

	res := EnsureUDP(8443, 0)
	if res.Firewall != "firewalld" || !res.Opened {
		t.Fatalf("结果不对: %+v", res)
	}
	args := argsOf(t, dir, "firewall-cmd")
	if !strings.Contains(args, "--permanent --add-port=8443/udp") {
		t.Errorf("缺少 --permanent 放行: %q", args)
	}
	if !strings.Contains(args, "--reload") {
		t.Errorf("缺少 --reload: %q", args)
	}
}

// TestEnsureUDPWithIptablesInserts 确认 iptables 没规则时会插入 ACCEPT。
func TestEnsureUDPWithIptablesInserts(t *testing.T) {
	dir := t.TempDir()
	// -C 失败（表示没有规则），其它成功
	makeTool(t, dir, "iptables", "case \"$1\" in -C) exit 1;; esac\nexit 0")
	t.Setenv("PATH", dir)

	res := EnsureUDP(8443, 0)
	if res.Firewall != "iptables" || !res.Opened {
		t.Fatalf("结果不对: %+v", res)
	}
	args := argsOf(t, dir, "iptables")
	if !strings.Contains(args, "-I INPUT -p udp --dport 8443 -j ACCEPT") {
		t.Errorf("应插入 ACCEPT 规则: %q", args)
	}
}

// TestEnsureUDPWithIptablesAlreadyOpen 确认已有规则时不再重复插入。
func TestEnsureUDPWithIptablesAlreadyOpen(t *testing.T) {
	dir := t.TempDir()
	makeTool(t, dir, "iptables", "exit 0")
	t.Setenv("PATH", dir)

	res := EnsureUDP(8443, 0)
	if res.Opened {
		t.Errorf("已有规则不应重复放行: %+v", res)
	}
	if !strings.Contains(res.Detail, "已有规则") {
		t.Errorf("说明文字不对: %q", res.Detail)
	}
	if strings.Contains(argsOf(t, dir, "iptables"), "-I INPUT") {
		t.Errorf("不应插入新规则")
	}
}

// TestEnsureUDPWithNft 确认 nftables 在已存在的 input 链里插规则。
func TestEnsureUDPWithNft(t *testing.T) {
	dir := t.TempDir()
	body := `case "$1 $2" in
  "-j list") echo '{"nftables":[{"metainfo":{}},{"table":{"family":"inet","name":"filter"}},{"chain":{"family":"inet","table":"filter","name":"input","type":"filter","hook":"input","policy":"accept"}}]}' ;;
  "list chain") echo 'table inet filter { chain input { type filter hook input priority filter; policy accept; } }' ;;
esac
exit 0`
	makeTool(t, dir, "nft", body)
	t.Setenv("PATH", dir)

	res := EnsureUDP(8443, 0)
	if res.Firewall != "nftables" || !res.Opened {
		t.Fatalf("结果不对: %+v", res)
	}
	args := argsOf(t, dir, "nft")
	if !strings.Contains(args, "add rule inet filter input udp dport 8443 accept comment hy2-panel") {
		t.Errorf("nft 规则不对: %q", args)
	}
}

// TestEnsureUDPWithNftNoInputChain 确认没有 input 链时什么都不做。
func TestEnsureUDPWithNftNoInputChain(t *testing.T) {
	dir := t.TempDir()
	body := `case "$1 $2" in
  "-j list") echo '{"nftables":[{"metainfo":{}},{"table":{"family":"inet","name":"hui_porthopping"}}]}' ;;
esac
exit 0`
	makeTool(t, dir, "nft", body)
	t.Setenv("PATH", dir)

	res := EnsureUDP(8443, 0)
	if res.Opened {
		t.Errorf("没有 input 链时不应改动任何东西: %+v", res)
	}
	if strings.Contains(argsOf(t, dir, "nft"), "add rule") {
		t.Errorf("不应插入规则")
	}
}

// TestEnsureUDPNoFirewall 确认服务器没有本地防火墙时安静返回。
func TestEnsureUDPNoFirewall(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PATH", dir)

	res := EnsureUDP(443, 0)
	if res.Firewall != "none" || res.Opened {
		t.Errorf("结果不对: %+v", res)
	}
}

// TestEnsureUDPInvalidPort 确认非法端口不会执行任何命令。
func TestEnsureUDPInvalidPort(t *testing.T) {
	dir := t.TempDir()
	makeTool(t, dir, "ufw", "exit 0")
	t.Setenv("PATH", dir)

	res := EnsureUDP(0, 0)
	if res.Opened || strings.Contains(res.Detail, "已通过") {
		t.Errorf("非法端口不应放行: %+v", res)
	}
	if argsOf(t, dir, "ufw") != "" {
		t.Errorf("非法端口不应执行 ufw")
	}
}