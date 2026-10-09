package updater

import "testing"

func TestPickAssetSelectsPlatformBinary(t *testing.T) {
	assets := []asset{
		{Name: "hysteria-linux-amd64.deb", URL: "deb"},
		{Name: "hysteria-windows-amd64.exe", URL: "win"},
		{Name: "hysteria-linux-amd64", URL: "bin-amd64"},
		{Name: "hysteria-linux-arm64", URL: "bin-arm64"},
		{Name: "hysteria-linux-arm64.rpm", URL: "rpm"},
	}

	a, err := pickAsset(assets, "linux", "amd64")
	if err != nil {
		t.Fatalf("应当匹配到 linux-amd64: %v", err)
	}
	if a.URL != "bin-amd64" {
		t.Errorf("应跳过 .deb 选择可执行文件，实际 %q", a.URL)
	}

	a, err = pickAsset(assets, "linux", "arm64")
	if err != nil {
		t.Fatalf("应当匹配到 linux-arm64: %v", err)
	}
	if a.URL != "bin-arm64" {
		t.Errorf("应跳过 .rpm 选择可执行文件，实际 %q", a.URL)
	}
}

func TestPickAssetNoMatch(t *testing.T) {
	assets := []asset{{Name: "hysteria-windows-amd64.exe", URL: "win"}}
	if _, err := pickAsset(assets, "linux", "amd64"); err == nil {
		t.Error("无匹配平台时应返回错误")
	}
}

func TestNormalizeVersion(t *testing.T) {
	cases := map[string]string{
		"v2.13.0":        "2.13.0",
		" 2.13.0 ":       "2.13.0",
		"v0.1.0":         "0.1.0",
		// 官方仓库使用 monorepo tag：必须能与之比较
		"app/v2.13.0":    "2.13.0",
		"app/v2.13.0\n":  "2.13.0",
		"core/v1.0.0":    "1.0.0",
	}
	for in, want := range cases {
		if got := normalizeVersion(in); got != want {
			t.Errorf("normalizeVersion(%q) = %q，期望 %q", in, got, want)
		}
	}
}

func TestCleanTag(t *testing.T) {
	cases := map[string]string{
		"app/v2.13.0":   "v2.13.0",
		"v2.13.0":       "v2.13.0",
		"  app/v1.2.3 ": "v1.2.3",
		"v1.2.3":        "v1.2.3",
	}
	for in, want := range cases {
		if got := cleanTag(in); got != want {
			t.Errorf("cleanTag(%q) = %q，期望 %q", in, got, want)
		}
	}
}

// 关键回归：本地 v2.13.0 与官方 tag app/v2.13.0 必须判定为同一版本
func TestVersionComparisonMatchesMonorepoTag(t *testing.T) {
	if normalizeVersion("v2.13.0") != normalizeVersion("app/v2.13.0") {
		t.Error("v2.13.0 与 app/v2.13.0 应视为同一版本")
	}
	if normalizeVersion("v2.12.0") == normalizeVersion("app/v2.13.0") {
		t.Error("不同版本不应判为相同")
	}
}

func TestTruncate(t *testing.T) {
	if got := truncate("hello", 10); got != "hello" {
		t.Errorf("短字符串不应截断，实际 %q", got)
	}
	if got := truncate("hello world", 5); got != "hello…" {
		t.Errorf("长字符串应截断并加省略号，实际 %q", got)
	}
}

func TestBackupPath(t *testing.T) {
	u := New("/usr/local/bin/hysteria", "hysteria-server")
	want := "/usr/local/bin/hysteria.bak"
	if u.BackupPath() != want {
		t.Errorf("备份路径期望 %q，实际 %q", want, u.BackupPath())
	}
}