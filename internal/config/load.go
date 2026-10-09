package config

import (
	"bytes"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Config 包装一份服务端配置。
//
// Raw 保存了解析后的完整顶层映射，用于向 UI 暴露“Panel 未识别的字段”。
// 设计取舍（见开发文档 51 节 Raw Config）：
//   - GUI 编辑走结构体，序列化时只输出结构体已知字段；
//   - 原始配置（Raw Config）编辑直接以文本形式提交官方 Core 校验，
//     因此官方新增的未知参数在 Raw 模式下不会丢失。
type Config struct {
	Server ServerConfig   `yaml:"-"`
	Raw    map[string]any `yaml:"-"`
}

// Load 从文件读取并解析 YAML 配置。
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取配置失败: %w", err)
	}
	return Parse(data)
}

// Parse 解析 YAML 字节。
func Parse(data []byte) (*Config, error) {
	var raw map[string]any
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("YAML 语法错误: %w", err)
	}
	if raw == nil {
		raw = map[string]any{}
	}

	var sc ServerConfig
	if err := yaml.Unmarshal(data, &sc); err != nil {
		return nil, fmt.Errorf("配置结构解析失败: %w", err)
	}

	return &Config{Server: sc, Raw: raw}, nil
}

// Marshal 将结构体序列化为 YAML（仅包含 Panel 已建模的字段）。
func (c *Config) Marshal() ([]byte, error) {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&c.Server); err != nil {
		return nil, fmt.Errorf("配置序列化失败: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("配置序列化失败: %w", err)
	}
	return buf.Bytes(), nil
}

// UnknownTopLevelKeys 返回 Raw 中存在、但结构体未建模的顶层字段名，
// 供 UI 提示用户“存在需在原始配置中维护的新字段”。
func (c *Config) UnknownTopLevelKeys() []string {
	known := map[string]any{}
	if b, err := c.Marshal(); err == nil {
		_ = yaml.Unmarshal(b, &known)
	}

	var out []string
	for k := range c.Raw {
		if k == "" {
			continue
		}
		if _, ok := known[k]; !ok {
			out = append(out, k)
		}
	}
	return out
}

// HasUnknownFields 是否存在 Panel 未建模的顶层字段。
func (c *Config) HasUnknownFields() bool {
	return len(c.UnknownTopLevelKeys()) > 0
}