package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// 开机自启的安装/卸载。
//
// 手写单元文件要建用户、拷文件、改三处路径、chown，六七步里错一步就是
// 开机起不来，而且往往到重启那天才发现。既然是单二进制，这些它自己做最省事。
//
// 只支持 systemd：自建商城基本都跑在 Linux 上，为 launchd 和 Windows 服务
// 各写一套的收益远低于维护成本。其它平台会明确说清楚，而不是装一半。

const unitPath = "/etc/systemd/system/moecard.service"

// serviceUser 是服务运行的身份。
//
// 不用 root：这个进程要处理公网请求，一旦被打穿，用哪个身份跑决定了
// 对方能拿到多少东西。
const serviceUser = "moecard"

// renderUnit 生成单元文件内容。
//
// scripts/moecard.service 里那份是给想手工改的人看的参考，内容由
// service_test.go 钉住，两边不会漂。
func renderUnit(exePath, workDir, user string) string {
	return `# MoeCard systemd 单元
#
# 由 moecard -install-service 生成。想手工改就直接编辑本文件，
# 改完 systemctl daemon-reload && systemctl restart moecard。

[Unit]
Description=MoeCard 数字商品自动发货商城
Documentation=https://github.com/qixishunzi/MoeCard
After=network-online.target
Wants=network-online.target

# 挂掉时别刷屏重启：60 秒内连挂 5 次就不再自动拉起。
# 这两个键必须放在 [Unit] 里 —— 写在 [Service] 里 systemd 只会打一行
# "Unknown key ... ignoring" 然后无视，配上 Restart=always 就是无限重启。
StartLimitIntervalSec=60
StartLimitBurst=5

[Service]
Type=simple

# 别用 root 跑。这个进程要处理公网请求，一旦被打穿，
# 用哪个身份跑决定了对方能拿到多少东西。
User=` + user + `
Group=` + user + `

WorkingDirectory=` + workDir + `
ExecStart=` + exePath + `

# 配置从工作目录的 .env 读，这里不用再传环境变量。
# 真要覆盖就用 Environment= 或者 EnvironmentFile=
#
# 注意 .env 里的 SQLITE_PATH / STORAGE_LOCAL_PATH 要用相对路径。
# 写成 /app/... 那种容器内的绝对路径，下面的 ReadWritePaths 挡不住它，
# 服务会因为写不了那个目录而起不来（.env.example 里默认已经是相对路径）。

Restart=always
RestartSec=5s

# 日志交给 journald：journalctl -u moecard -f
StandardOutput=journal
StandardError=journal

# ---- 沙箱 ----
# 这些是「万一被 RCE 了，对方能干什么」的边界。
# 自更新要写自己所在的目录，所以 ProtectSystem 用 full 而不是 strict，
# 并把工作目录显式放进 ReadWritePaths。
ProtectSystem=full
ReadWritePaths=` + workDir + `
ProtectHome=true
PrivateTmp=true
NoNewPrivileges=true
ProtectKernelTunables=true
ProtectKernelModules=true
ProtectControlGroups=true
RestrictSUIDSGID=true
RestrictNamespaces=true
LockPersonality=true
MemoryDenyWriteExecute=false
RestrictAddressFamilies=AF_INET AF_INET6 AF_UNIX

# 想让 80/443 直接由它监听（不套 Nginx）时解开这一行，
# 否则非 root 用户绑不了 1024 以下的端口
# AmbientCapabilities=CAP_NET_BIND_SERVICE

[Install]
WantedBy=multi-user.target
`
}

// requireSystemd 检查当前环境能不能装服务。
func requireSystemd() error {
	if runtime.GOOS != "linux" {
		return fmt.Errorf("开机自启只支持 Linux 的 systemd，当前是 %s。\n"+
			"macOS 请用 launchd，Windows 请用「任务计划程序」或 nssm", runtime.GOOS)
	}
	// systemd 运行时一定有这个目录，比看 /sbin/init 可靠
	if _, err := os.Stat("/run/systemd/system"); err != nil {
		return fmt.Errorf("没有检测到 systemd（/run/systemd/system 不存在）。\n" +
			"用 OpenRC / SysVinit 的系统请手工写启动脚本")
	}
	if os.Geteuid() != 0 {
		return fmt.Errorf("需要 root 权限，请用 sudo 重跑")
	}
	if _, err := exec.LookPath("systemctl"); err != nil {
		return fmt.Errorf("找不到 systemctl: %w", err)
	}
	return nil
}

// installService 安装并启用开机自启。
func installService() error {
	if err := requireSystemd(); err != nil {
		return err
	}

	exePath, err := os.Executable()
	if err != nil {
		return err
	}
	if exePath, err = filepath.EvalSymlinks(exePath); err != nil {
		return err
	}
	workDir := filepath.Dir(exePath)

	// 已经装过就先提示，别默默覆盖别人改过的单元文件
	if _, err := os.Stat(unitPath); err == nil {
		fmt.Printf("%s 已存在，将被覆盖。\n", unitPath)
		fmt.Println("（改过的内容会丢失，需要保留的话先备份）")
	}

	fmt.Printf("可执行文件 %s\n", exePath)
	fmt.Printf("工作目录   %s\n", workDir)
	fmt.Printf("运行身份   %s\n\n", serviceUser)

	if err := ensureUser(serviceUser, workDir); err != nil {
		return err
	}

	fmt.Println("正在设置目录权限")
	if err := runCmd("chown", "-R", serviceUser+":"+serviceUser, workDir); err != nil {
		return fmt.Errorf("chown 失败: %w", err)
	}

	fmt.Println("正在写入", unitPath)
	if err := os.WriteFile(unitPath, []byte(renderUnit(exePath, workDir, serviceUser)), 0o644); err != nil {
		return fmt.Errorf("写入单元文件失败: %w", err)
	}

	fmt.Println("正在启用并启动")
	if err := runCmd("systemctl", "daemon-reload"); err != nil {
		return err
	}
	if err := runCmd("systemctl", "enable", "moecard"); err != nil {
		return err
	}
	if err := runCmd("systemctl", "restart", "moecard"); err != nil {
		return fmt.Errorf("启动失败: %w\n用 journalctl -u moecard -n 30 看原因", err)
	}

	fmt.Println("\n完成。开机会自动启动。")
	fmt.Println("  查看状态  systemctl status moecard")
	fmt.Println("  看日志    journalctl -u moecard -f")
	fmt.Println("  重启      systemctl restart moecard")
	fmt.Println("  取消自启  moecard -uninstall-service")
	return nil
}

// ensureUser 建一个专用的系统用户（已存在就跳过）。
func ensureUser(name, home string) error {
	if err := exec.Command("id", name).Run(); err == nil {
		fmt.Printf("用户 %s 已存在\n", name)
		return nil
	}
	fmt.Printf("正在创建用户 %s\n", name)

	// nologin 的路径各发行版不一样，挨个找
	shell := "/usr/sbin/nologin"
	for _, p := range []string{"/usr/sbin/nologin", "/sbin/nologin", "/bin/false"} {
		if _, err := os.Stat(p); err == nil {
			shell = p
			break
		}
	}
	if err := runCmd("useradd", "--system", "--shell", shell, "--home-dir", home,
		"--no-create-home", name); err != nil {
		return fmt.Errorf("创建用户失败: %w", err)
	}
	return nil
}

// uninstallService 停止并移除服务。数据一律不动。
func uninstallService() error {
	if err := requireSystemd(); err != nil {
		return err
	}
	if _, err := os.Stat(unitPath); err != nil {
		fmt.Println("没有安装过服务，无需卸载。")
		return nil
	}

	fmt.Println("正在停止并禁用")
	// 停不掉也继续往下走：目标是把单元文件清掉，
	// 半路退出只会留下一个「禁用了但文件还在」的中间状态
	_ = exec.Command("systemctl", "stop", "moecard").Run()
	_ = exec.Command("systemctl", "disable", "moecard").Run()

	fmt.Println("正在删除", unitPath)
	if err := os.Remove(unitPath); err != nil {
		return err
	}
	if err := runCmd("systemctl", "daemon-reload"); err != nil {
		return err
	}

	fmt.Println("\n已取消开机自启。")
	fmt.Printf("数据、配置和 %s 用户都保留着，需要的话自己删：\n", serviceUser)
	fmt.Printf("  userdel %s\n", serviceUser)
	return nil
}

// runCmd 执行外部命令，失败时把 stderr 一起带出来。
//
// 叫 runCmd 而不是 run —— main.go 里已经有一个 run(configPath, skipMigrate)。
func runCmd(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			return fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
		}
		return fmt.Errorf("%s %s: %w\n  %s", name, strings.Join(args, " "), err, msg)
	}
	return nil
}
