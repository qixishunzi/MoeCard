package database

import (
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/moecard/server/internal/logger"
	"github.com/moecard/server/migrations"
)

// Migration 代表一个迁移文件。
type Migration struct {
	Version string
	Name    string
	SQL     string
}

// AppliedMigration 是 schema_migrations 表中的一行。
type AppliedMigration struct {
	Version   string    `gorm:"column:version;primaryKey;size:64" json:"version"`
	Name      string    `gorm:"column:name;size:191" json:"name"`
	AppliedAt time.Time `gorm:"column:applied_at" json:"applied_at"`
}

func (AppliedMigration) TableName() string { return "schema_migrations" }

// Migrate 执行所有未应用的迁移。
//
// 迁移文件按驱动分目录，命名规则 NNNN_name.sql，按文件名升序执行。
// 每个文件在**一个事务内**整体执行，失败则回滚，绝不留下半截 schema。
func (d *DB) Migrate() error {
	if err := d.ensureMigrationTable(); err != nil {
		return err
	}

	all, err := loadMigrations(d.driver)
	if err != nil {
		return err
	}

	var rows []AppliedMigration
	if err := d.DB.Find(&rows).Error; err != nil {
		return fmt.Errorf("load applied migrations: %w", err)
	}
	applied := make(map[string]bool, len(rows))
	for _, r := range rows {
		applied[r.Version] = true
	}

	pending := 0
	for _, m := range all {
		if applied[m.Version] {
			continue
		}
		pending++
		start := time.Now()

		// 每条语句单独执行：SQLite 驱动不支持一次 Exec 多条语句。
		stmts := splitStatements(m.SQL)
		txErr := d.DB.Transaction(func(tx *gorm.DB) error {
			for i, s := range stmts {
				if err := tx.Exec(s).Error; err != nil {
					return fmt.Errorf("statement #%d failed: %w\nSQL: %s", i+1, err, truncate(s, 300))
				}
			}
			return tx.Create(&AppliedMigration{
				Version:   m.Version,
				Name:      m.Name,
				AppliedAt: time.Now().UTC(),
			}).Error
		})
		if txErr != nil {
			return fmt.Errorf("migration %s_%s: %w", m.Version, m.Name, txErr)
		}
		logger.L().Info("migration applied",
			"version", m.Version, "name", m.Name, "elapsed_ms", time.Since(start).Milliseconds())
	}

	if pending == 0 {
		logger.L().Info("database schema up to date", "version", currentVersion(all))
	} else {
		logger.L().Info("migrations completed", "applied", pending, "version", currentVersion(all))
	}
	return nil
}

// MigrationStatus 返回已应用与待应用的迁移列表，供 cmd/migrate status 使用。
func (d *DB) MigrationStatus() (appliedList []AppliedMigration, pendingList []Migration, err error) {
	if err = d.ensureMigrationTable(); err != nil {
		return nil, nil, err
	}
	all, err := loadMigrations(d.driver)
	if err != nil {
		return nil, nil, err
	}
	if err = d.DB.Order("version asc").Find(&appliedList).Error; err != nil {
		return nil, nil, err
	}
	set := make(map[string]bool, len(appliedList))
	for _, a := range appliedList {
		set[a.Version] = true
	}
	for _, m := range all {
		if !set[m.Version] {
			pendingList = append(pendingList, m)
		}
	}
	return appliedList, pendingList, nil
}

func (d *DB) ensureMigrationTable() error {
	var ddl string
	if d.IsSQLite() {
		ddl = `CREATE TABLE IF NOT EXISTS schema_migrations (
	version    TEXT PRIMARY KEY,
	name       TEXT NOT NULL DEFAULT '',
	applied_at DATETIME NOT NULL
)`
	} else {
		ddl = "CREATE TABLE IF NOT EXISTS `schema_migrations` (" +
			"`version` VARCHAR(64) NOT NULL," +
			"`name` VARCHAR(191) NOT NULL DEFAULT ''," +
			"`applied_at` DATETIME NOT NULL," +
			"PRIMARY KEY (`version`)" +
			") ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci"
	}
	if err := d.DB.Exec(ddl).Error; err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}
	return nil
}

func loadMigrations(driver string) ([]Migration, error) {
	dir := driver
	entries, err := fs.ReadDir(migrations.FS, dir)
	if err != nil {
		return nil, fmt.Errorf("read migrations dir %s: %w", dir, err)
	}

	var out []Migration
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		raw, err := migrations.FS.ReadFile(path.Join(dir, e.Name()))
		if err != nil {
			return nil, fmt.Errorf("read migration %s: %w", e.Name(), err)
		}
		base := strings.TrimSuffix(e.Name(), ".sql")
		version, name, _ := strings.Cut(base, "_")
		out = append(out, Migration{Version: version, Name: name, SQL: string(raw)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Version < out[j].Version })
	return out, nil
}

func currentVersion(ms []Migration) string {
	if len(ms) == 0 {
		return "none"
	}
	return ms[len(ms)-1].Version
}

// splitStatements 按分号切分 SQL，正确跳过字符串字面量与注释。
func splitStatements(sqlText string) []string {
	var (
		out     []string
		cur     strings.Builder
		inStr   bool
		strCh   byte
		inLineC bool
		inBlkC  bool
	)
	for i := 0; i < len(sqlText); i++ {
		ch := sqlText[i]

		switch {
		case inLineC:
			if ch == '\n' {
				inLineC = false
				cur.WriteByte(ch)
			}
			continue
		case inBlkC:
			if ch == '*' && i+1 < len(sqlText) && sqlText[i+1] == '/' {
				inBlkC = false
				i++
			}
			continue
		case inStr:
			cur.WriteByte(ch)
			if ch == '\\' && i+1 < len(sqlText) {
				i++
				cur.WriteByte(sqlText[i])
				continue
			}
			if ch == strCh {
				inStr = false
			}
			continue
		}

		if ch == '-' && i+1 < len(sqlText) && sqlText[i+1] == '-' {
			inLineC = true
			i++
			continue
		}
		if ch == '/' && i+1 < len(sqlText) && sqlText[i+1] == '*' {
			inBlkC = true
			i++
			continue
		}
		if ch == '\'' || ch == '"' || ch == '`' {
			inStr, strCh = true, ch
			cur.WriteByte(ch)
			continue
		}
		if ch == ';' {
			if s := strings.TrimSpace(cur.String()); s != "" {
				out = append(out, s)
			}
			cur.Reset()
			continue
		}
		cur.WriteByte(ch)
	}
	if s := strings.TrimSpace(cur.String()); s != "" {
		out = append(out, s)
	}
	return out
}
