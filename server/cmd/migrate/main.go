// Command migrate 是独立的数据库迁移工具。
//
// 用法：
//
//	go run ./cmd/migrate            # 执行所有未应用的迁移
//	go run ./cmd/migrate -status    # 查看迁移状态
//	go run ./cmd/migrate -create-admin -u admin -p 'YourStrongPass123'
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/moecard/server/internal/config"
	"github.com/moecard/server/internal/database"
	"github.com/moecard/server/internal/logger"
	"github.com/moecard/server/internal/model"
	"github.com/moecard/server/internal/repository"
	"github.com/moecard/server/internal/service"
	"github.com/moecard/server/internal/utils"
)

func main() {
	var (
		configPath  = flag.String("config", "", "配置文件路径")
		showStatus  = flag.Bool("status", false, "显示迁移状态")
		createAdmin = flag.Bool("create-admin", false, "创建管理员账号")
		username    = flag.String("u", "", "管理员用户名")
		password    = flag.String("p", "", "管理员密码")
		backup      = flag.Bool("backup", false, "备份数据库（SQLite 一致性快照）")
		backupTo    = flag.String("backup-to", "", "备份文件路径，默认 backups/moecard-<时间>.db")
		encryptCode = flag.Bool("encrypt-codes", false, "把历史明文卡密加密（需先配置 DATA_ENCRYPTION_KEY）")
	)
	flag.Parse()

	opts := runOptions{
		showStatus: *showStatus, createAdmin: *createAdmin,
		username: *username, password: *password,
		backup: *backup, backupTo: *backupTo, encryptCodes: *encryptCode,
	}
	if err := run(*configPath, opts); err != nil {
		fmt.Fprintf(os.Stderr, "错误: %v\n", err)
		os.Exit(1)
	}
}

// runOptions 汇总子命令参数，避免 run() 的形参列表越加越长。
type runOptions struct {
	showStatus   bool
	createAdmin  bool
	username     string
	password     string
	backup       bool
	backupTo     string
	encryptCodes bool
}

func run(configPath string, opts runOptions) error {
	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}
	if err := logger.Init(logger.Config{Level: cfg.Log.Level, Format: cfg.Log.Format}); err != nil {
		return err
	}
	defer logger.Close()

	// 加密密钥要在任何数据读写之前就绪
	if err := utils.InitDataEncryption(cfg.App.DataKey); err != nil {
		return fmt.Errorf("初始化数据加密失败: %w", err)
	}

	db, err := database.Open(cfg)
	if err != nil {
		return err
	}
	defer db.Close()

	if opts.showStatus {
		return printStatus(db)
	}
	// 备份不改结构，先于迁移执行 —— 万一迁移出问题，手里已经有一份旧数据
	if opts.backup {
		return doBackup(cfg, db, opts.backupTo)
	}

	if err := db.Migrate(); err != nil {
		return fmt.Errorf("迁移失败: %w", err)
	}
	fmt.Println("✓ 数据库迁移完成")

	if opts.encryptCodes {
		return doEncryptCodes(db)
	}
	if opts.createAdmin {
		return doCreateAdmin(cfg, db, opts.username, opts.password)
	}
	return nil
}

func printStatus(db *database.DB) error {
	applied, pending, err := db.MigrationStatus()
	if err != nil {
		return err
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 4, 3, ' ', 0)
	fmt.Fprintf(w, "驱动:\t%s\n\n", db.Driver())
	fmt.Fprintln(w, "已应用\t版本\t名称\t时间")
	for _, a := range applied {
		fmt.Fprintf(w, "  ✓\t%s\t%s\t%s\n",
			a.Version, a.Name, a.AppliedAt.Format("2006-01-02 15:04:05"))
	}
	if len(pending) > 0 {
		fmt.Fprintln(w, "\n待应用\t版本\t名称")
		for _, p := range pending {
			fmt.Fprintf(w, "  ○\t%s\t%s\n", p.Version, p.Name)
		}
	} else {
		fmt.Fprintln(w, "\n没有待应用的迁移。")
	}
	return w.Flush()
}

func doCreateAdmin(cfg *config.Config, db *database.DB, username, password string) error {
	if username == "" || password == "" {
		return fmt.Errorf("请通过 -u 与 -p 指定用户名和密码")
	}
	// 强度校验前置，避免生成一个弱口令管理员
	if err := utils.ValidatePasswordStrength(password); err != nil {
		return fmt.Errorf("密码不符合要求: %w", err)
	}

	repos := repository.New(db)
	svc, err := service.New(cfg, db, repos)
	if err != nil {
		return err
	}

	admin, err := svc.Admin.CreateAdmin(context.Background(), &service.AdminInput{
		Username: username,
		Password: password,
		Nickname: "管理员",
		Status:   model.StatusActive,
	})
	if err != nil {
		return err
	}
	// 顺带把 installed 标记打上，避免创建完账号还被 /setup 拦住
	if err := svc.Setting.Set(context.Background(), model.SetInstalled, "1"); err != nil {
		logger.L().Warn("写入 installed 标记失败", "err", err)
	}

	fmt.Printf("✓ 管理员创建成功: %s (id=%d)\n", admin.Username, admin.ID)
	return nil
}
