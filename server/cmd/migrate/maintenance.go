package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gorm.io/gorm"

	"github.com/moecard/server/internal/config"
	"github.com/moecard/server/internal/database"
	"github.com/moecard/server/internal/model"
	"github.com/moecard/server/internal/utils"
)

// doBackup 生成数据库一致性快照。
//
// SQLite 用 VACUUM INTO：这是官方推荐的热备份方式，
// 会拿一个读事务保证快照内部一致，且不会阻塞写入太久。
// 直接 cp 数据库文件是错的 —— WAL 模式下会拷到一个撕裂的中间状态。
//
// MySQL 无法在进程内做等价的事，返回明确指引而不是假装成功。
func doBackup(cfg *config.Config, db *database.DB, dest string) error {
	if db.Driver() != config.DriverSQLite {
		return fmt.Errorf(
			"当前使用 MySQL，请用 mysqldump 备份：\n" +
				"  mysqldump -h主机 -u用户 -p 数据库名 > backup.sql")
	}

	if dest == "" {
		dest = filepath.Join("backups",
			fmt.Sprintf("moecard-%s.db", time.Now().Format("20060102-150405")))
	}
	abs, err := filepath.Abs(dest)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return fmt.Errorf("创建备份目录失败: %w", err)
	}
	// VACUUM INTO 要求目标文件不存在
	if _, err := os.Stat(abs); err == nil {
		return fmt.Errorf("目标文件已存在: %s", abs)
	}

	// 目标路径要作为 SQL 字面量传入，单引号必须转义
	quoted := "'" + escapeSQLLiteral(abs) + "'"
	if err := db.DB.Exec("VACUUM INTO " + quoted).Error; err != nil {
		return fmt.Errorf("备份失败: %w", err)
	}

	info, err := os.Stat(abs)
	if err != nil {
		return fmt.Errorf("备份文件未生成: %w", err)
	}
	fmt.Printf("✓ 备份完成: %s (%.2f MB)\n", abs, float64(info.Size())/(1<<20))
	fmt.Println()
	fmt.Println("提示：备份文件里含有全部订单与卡密。")
	if utils.DataEncryptionEnabled() {
		fmt.Println("      卡密为加密存储，但仍请妥善保管备份并单独保存 DATA_ENCRYPTION_KEY —— ")
		fmt.Println("      密钥丢失将无法从备份中恢复任何卡密。")
	} else {
		fmt.Println("      当前未启用静态加密，备份中的卡密是明文，请务必加密保存备份文件。")
	}
	return nil
}

func escapeSQLLiteral(s string) string {
	out := make([]rune, 0, len(s)+4)
	for _, r := range s {
		if r == '\'' {
			out = append(out, '\'')
		}
		out = append(out, r)
	}
	return string(out)
}

// doEncryptCodes 把历史明文卡密就地加密。
//
// 用于"先跑了一段时间才配置 DATA_ENCRYPTION_KEY"的场景。
// 幂等：已经是密文的行会被跳过，重复执行没有副作用。
func doEncryptCodes(db *database.DB) error {
	if !utils.DataEncryptionEnabled() {
		return fmt.Errorf("未配置 DATA_ENCRYPTION_KEY，无法加密。\n" +
			"  请先生成密钥并写入环境变量或 .env：\n" +
			"    openssl rand -hex 32")
	}

	ctx := context.Background()
	var total, done int64

	if err := db.DB.WithContext(ctx).Model(&model.ProductCode{}).
		Where("encrypted = ?", false).Count(&total).Error; err != nil {
		return err
	}
	if total == 0 {
		fmt.Println("✓ 没有需要加密的卡密（全部已是密文）")
		return nil
	}
	fmt.Printf("发现 %d 条明文卡密，开始加密...\n", total)

	const batch = 500
	for {
		var rows []model.ProductCode
		if err := db.DB.WithContext(ctx).
			Where("encrypted = ?", false).
			Order("id ASC").Limit(batch).Find(&rows).Error; err != nil {
			return err
		}
		if len(rows) == 0 {
			break
		}

		// 每批一个事务：中途失败时已完成的批次不会回滚，
		// 重跑命令会从断点继续（encrypted 标记就是断点）。
		err := db.Tx(ctx, func(tx *gorm.DB) error {
			for _, r := range rows {
				if utils.IsEncrypted(r.Content) {
					// 内容已是密文但标记没跟上，补一下标记即可
					if err := tx.Model(&model.ProductCode{}).Where("id = ?", r.ID).
						Update("encrypted", true).Error; err != nil {
						return err
					}
					continue
				}
				enc, err := utils.Encrypt(r.Content)
				if err != nil {
					return fmt.Errorf("加密失败(id=%d): %w", r.ID, err)
				}
				if err := tx.Model(&model.ProductCode{}).Where("id = ?", r.ID).
					Updates(map[string]any{"content": enc, "encrypted": true}).Error; err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			return err
		}

		done += int64(len(rows))
		fmt.Printf("  已处理 %d / %d\n", done, total)
		if len(rows) < batch {
			break
		}
	}

	fmt.Printf("✓ 加密完成，共处理 %d 条\n", done)
	fmt.Println()
	fmt.Println("⚠ 请务必备份 DATA_ENCRYPTION_KEY —— 密钥丢失后这些卡密将永久无法读取。")
	return nil
}
