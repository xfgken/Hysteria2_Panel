// Package db 封装 Panel 的 SQLite 持久层。
//
// 仅使用纯 Go 驱动 modernc.org/sqlite，保证单二进制、无 cgo、便于交叉编译，
// 契合“VPS 友好、单二进制部署”的目标（开发文档 55 / 56 节）。
package db

import (
	"database/sql"
	_ "embed"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

//go:embed schema.sql
var schemaSQL string

// DB 是对 *sql.DB 的轻量封装。
type DB struct {
	*sql.DB
}

// Open 打开（必要时创建）数据库并执行迁移。
func Open(path string) (*DB, error) {
	dsn := fmt.Sprintf(
		"file:%s?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(ON)&_pragma=synchronous(NORMAL)",
		path,
	)
	sqldb, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("打开数据库失败: %w", err)
	}
	if err := sqldb.Ping(); err != nil {
		return nil, fmt.Errorf("连接数据库失败: %w", err)
	}

	d := &DB{sqldb}
	if err := d.migrate(); err != nil {
		return nil, err
	}
	return d, nil
}

// migrate 执行内嵌的结构脚本（全部使用 IF NOT EXISTS，可重复执行）。
func (d *DB) migrate() error {
	if _, err := d.Exec(schemaSQL); err != nil {
		return fmt.Errorf("数据库迁移失败: %w", err)
	}
	return nil
}

// Close 关闭数据库连接。
func (d *DB) Close() error { return d.DB.Close() }

// now 返回统一使用的 RFC3339 时间字符串。
func now() string { return time.Now().Format(time.RFC3339) }