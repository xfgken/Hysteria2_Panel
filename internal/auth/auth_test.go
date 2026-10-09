package auth

import "testing"

func TestHashAndCheckPassword(t *testing.T) {
	hash, err := HashPassword("s3cret-password")
	if err != nil {
		t.Fatalf("生成哈希失败: %v", err)
	}
	if hash == "s3cret-password" {
		t.Fatal("不得明文存储密码")
	}
	if !CheckPassword(hash, "s3cret-password") {
		t.Error("正确密码应校验通过")
	}
	if CheckPassword(hash, "wrong") {
		t.Error("错误密码不应通过")
	}
	if CheckPassword("not-a-hash", "whatever") {
		t.Error("非法哈希不应通过")
	}
}

func TestHashIsSalted(t *testing.T) {
	h1, _ := HashPassword("same")
	h2, _ := HashPassword("same")
	if h1 == h2 {
		t.Error("bcrypt 每次应产生不同盐值")
	}
}

func TestSecureCompare(t *testing.T) {
	if !SecureCompare("abc", "abc") {
		t.Error("相同字符串应返回 true")
	}
	if SecureCompare("abc", "abd") {
		t.Error("不同字符串应返回 false")
	}
	if SecureCompare("abc", "abcd") {
		t.Error("长度不同应返回 false")
	}
}