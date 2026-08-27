package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/moecard/server/internal/selfupdate"
)

// runUpdate 是 `moecard -update` 的实现。
//
// 放在命令行而不是只做成后台按钮：换掉可执行文件之后必须重启进程才生效，
// 而重启这件事该由 systemd / Docker / 或者人自己来做，程序自己重启自己
// 在三个操作系统上各有各的坑，出问题时还很难看出发生了什么。
// 后台页面负责"告诉你有新版本"，落地动作在这里。
func runUpdate(current string, checkOnly, assumeYes bool) error {
	cli := selfupdate.New(current)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	fmt.Println("正在检查更新…")
	res, err := cli.Check(ctx)
	if err != nil {
		return err
	}

	fmt.Printf("当前版本 %s，最新版本 %s\n", res.Current, res.Latest)
	if !res.HasUpdate {
		fmt.Println("已经是最新版本，无需更新。")
		return nil
	}
	if !res.Supported {
		if checkOnly {
			fmt.Println("注意：" + res.Reason)
			return nil
		}
		return fmt.Errorf("%s", res.Reason)
	}

	if notes := strings.TrimSpace(res.Release.Notes); notes != "" {
		fmt.Println("\n更新内容：")
		for _, line := range strings.Split(notes, "\n") {
			fmt.Println("  " + line)
		}
	}
	fmt.Printf("\n发布页：%s\n", res.Release.URL)

	if !assumeYes && !confirm(fmt.Sprintf("确认更新到 %s 吗？[y/N] ", res.Latest)) {
		fmt.Println("已取消。")
		return nil
	}

	old, err := cli.Apply(ctx, res.Release, func(msg string) {
		fmt.Println("  " + msg)
	})
	if err != nil {
		return err
	}

	fmt.Printf("\n更新完成，已升级到 %s\n", res.Latest)
	fmt.Printf("旧版本备份在 %s，会一直留到下次更新。\n", old)
	fmt.Println("新版本要是有问题，把备份改名回来就能回滚。")
	fmt.Println("\n重启服务后生效：")
	fmt.Println("  systemd:  sudo systemctl restart moecard")
	fmt.Println("  手动启动: 停掉当前进程再重新运行")
	return nil
}

// confirm 读一行确认。
//
// 标准输入不是终端时（脚本、CI）直接当成"没确认"：
// 在无人值守的环境里悄悄换掉可执行文件，比不更新危险得多。
func confirm(prompt string) bool {
	fi, err := os.Stdin.Stat()
	if err != nil || fi.Mode()&os.ModeCharDevice == 0 {
		fmt.Println("检测到非交互环境，已跳过更新。需要无人值守时加 -y 参数。")
		return false
	}
	fmt.Print(prompt)
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return true
	}
	return false
}
