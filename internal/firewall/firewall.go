// Package firewall 让服务器本地防火墙自动放行 Hysteria 的监听端口。
//
// 面板以 root 身份运行，因此配置生效时可以顺手把端口放行掉：
// 换了监听端口之后「连上了却不通」，最常见的原因就是本地防火墙没放行，
// 而这个坑完全没必要让用户上服务器手动敲命令。
//
// 支持的防火墙（检测到哪个用哪个，优先级从上到下）：
//
//	ufw        ufw allow <port>/udp（本身幂等，重复执行无害）
//	firewalld  firewall-cmd --permanent --add-port=<port>/udp + --reload
//	iptables   先用 -C 查，没有才 -I INPUT 插一条 ACCEPT
//	nftables   只在**已经存在的 filter input 链**里插一条带注释的规则
//
// 什么都没检测到（说明服务器本来就没有本地防火墙）就什么都不做 ——
// 这种情况下乱加规则反而可能把自己关在外面。
//
// 注意：这里只管服务器**本机**的防火墙。如果拦截发生在云厂商的
// 安全组 / 云防火墙那一层，只能去厂商控制台放行，面板无能为力。
package firewall

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// comment 是面板插入 nftables 规则时带的注释，用于避免重复插入。
const comment = "hy2-panel"

// Result 是一次端口放行的结果，用于写日志与回给前端。
type Result struct {
	Firewall string `json:"firewall"` // ufw / firewalld / iptables / nftables / none
	Opened   bool   `json:"opened"`   // 本次是否真的执行了放行
	Detail   string `json:"detail"`   // 人类可读的说明
}

// EnsureUDP 放行 UDP 端口；end 大于 start 时按整段范围处理（端口跳跃用）。
//
// 传入非法端口时直接返回，不做任何改动。
func EnsureUDP(start, end int) Result {
	if start <= 0 || start > 65535 {
		return Result{Firewall: "none", Detail: "监听端口不合法，跳过防火墙放行"}
	}
	if end <= 0 || end > 65535 || end < start {
		end = start
	}

	if _, err := exec.LookPath("ufw"); err == nil {
		if err := run("ufw", "allow", ufwSpec(start, end)); err != nil {
			return Result{Firewall: "ufw", Detail: "ufw 放行失败: " + firstLine(err.Error())}
		}
		return Result{Firewall: "ufw", Opened: true, Detail: "已通过 ufw 放行 UDP " + rangeText(start, end)}
	}

	if _, err := exec.LookPath("firewall-cmd"); err == nil {
		if err := run("firewall-cmd", "--permanent", "--add-port="+portSpec(start, end, "-")+"/udp"); err != nil {
			return Result{Firewall: "firewalld", Detail: "firewalld 放行失败: " + firstLine(err.Error())}
		}
		_ = run("firewall-cmd", "--reload")
		return Result{Firewall: "firewalld", Opened: true, Detail: "已通过 firewalld 放行 UDP " + rangeText(start, end)}
	}

	if _, err := exec.LookPath("iptables"); err == nil {
		dport := portSpec(start, end, ":")
		// 已有规则就别重复插了。
		if err := run("iptables", "-C", "INPUT", "-p", "udp", "--dport", dport, "-j", "ACCEPT"); err == nil {
			return Result{Firewall: "iptables", Detail: "iptables 已有规则，无需重复放行（UDP " + rangeText(start, end) + "）"}
		}
		if err := run("iptables", "-I", "INPUT", "-p", "udp", "--dport", dport, "-j", "ACCEPT"); err != nil {
			return Result{Firewall: "iptables", Detail: "iptables 放行失败: " + firstLine(err.Error())}
		}
		return Result{Firewall: "iptables", Opened: true, Detail: "已通过 iptables 放行 UDP " + rangeText(start, end)}
	}

	if _, err := exec.LookPath("nft"); err == nil {
		if detail, ok := nftEnsure(start, end); ok {
			return Result{Firewall: "nftables", Opened: true, Detail: detail}
		}
		return Result{Firewall: "none", Detail: "未发现需要放行的本地防火墙（nftables 无 input 链）"}
	}

	return Result{Firewall: "none", Detail: "未检测到本地防火墙，无需放行"}
}

// nftEnsure 在已存在的 filter input 链里插一条放行规则。
//
// 找不到 input 链就返回 false，由调用方按「没有防火墙」处理。
func nftEnsure(start, end int) (string, bool) {
	family, table, chain, ok := nftInputChain()
	if !ok {
		return "", false
	}

	spec := portSpec(start, end, "-")
	// 先看有没有自己之前插过的规则，避免每次应用配置都堆一条。
	if out, err := exec.Command("nft", "list", "chain", family, table, chain).Output(); err == nil {
		body := string(out)
		if strings.Contains(body, comment) && strings.Contains(body, "dport "+spec+" ") {
			return "nftables 已有规则，无需重复放行（UDP " + rangeText(start, end) + "）", false
		}
	}

	if err := run("nft", "add", "rule", family, table, chain,
		"udp", "dport", spec, "accept", "comment", comment); err != nil {
		return "nftables 放行失败: " + firstLine(err.Error()), false
	}
	return "已通过 nftables 放行 UDP " + rangeText(start, end), true
}

// nftInputChain 找出系统里第一个 filter 类型的 input 钩子链。
func nftInputChain() (family, table, chain string, ok bool) {
	out, err := exec.Command("nft", "-j", "list", "chains").Output()
	if err != nil {
		return "", "", "", false
	}
	var doc struct {
		Nftables []map[string]json.RawMessage `json:"nftables"`
	}
	if err := json.Unmarshal(out, &doc); err != nil {
		return "", "", "", false
	}
	for _, item := range doc.Nftables {
		raw, has := item["chain"]
		if !has {
			continue
		}
		var ch struct {
			Family string `json:"family"`
			Table  string `json:"table"`
			Name   string `json:"name"`
			Type   string `json:"type"`
			Hook   string `json:"hook"`
		}
		if err := json.Unmarshal(raw, &ch); err != nil {
			continue
		}
		if ch.Type == "filter" && ch.Hook == "input" && ch.Family != "" && ch.Table != "" && ch.Name != "" {
			return ch.Family, ch.Table, ch.Name, true
		}
	}
	return "", "", "", false
}

// ufwSpec 生成 ufw 的端口写法（范围用冒号，例如 1000:2000/udp）。
func ufwSpec(start, end int) string {
	if end > start {
		return fmt.Sprintf("%d:%d/udp", start, end)
	}
	return strconv.Itoa(start) + "/udp"
}

// portSpec 生成端口写法，范围为 "起始<sep>结束"。
func portSpec(start, end int, sep string) string {
	if end > start {
		return fmt.Sprintf("%d%s%d", start, sep, end)
	}
	return strconv.Itoa(start)
}

func rangeText(start, end int) string {
	if end > start {
		return fmt.Sprintf("%d-%d", start, end)
	}
	return strconv.Itoa(start)
}

// run 执行命令，失败时把输出一起带回，方便定位。
func run(name string, args ...string) error {
	out, err := exec.Command(name, args...).CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			return err
		}
		return fmt.Errorf("%w: %s", err, msg)
	}
	return nil
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return s
}
