// Package migrations 以 embed 的方式携带所有迁移 SQL，
// 使编译产物是真正的单文件二进制（不需要外部 .sql 文件）。
//
// 目录约定：
//
//	migrations/sqlite/NNNN_name.sql
//	migrations/mysql/NNNN_name.sql
//
// 两个驱动各自维护一份 DDL —— AUTOINCREMENT / AUTO_INCREMENT、
// 索引长度限制、DATETIME 精度等差异无法用一套 DDL 覆盖。
// 把差异封在这里，业务代码就彻底不需要感知数据库类型。
package migrations

import "embed"

//go:embed all:sqlite all:mysql
var FS embed.FS
