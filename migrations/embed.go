// Package migrations 通过 go:embed 将 schema.sql 打包进二进制，
// 服务启动时幂等执行建表语句（所有语句均使用 IF NOT EXISTS）。
package migrations

import _ "embed"

// Schema 是完整的数据表结构定义，启动时由 cmd/server 一次性执行。
//
//go:embed schema.sql
var Schema string
