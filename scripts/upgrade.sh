#!/usr/bin/env sh
#
# MoeCard 升级脚本
#
#   cd /opt/moecard && sudo sh scripts/upgrade.sh
#
# 和直接跑 `moecard -update` 的区别：这个脚本管服务的停启和回滚。
# 二进制自己不该去重启承载它的服务 —— 更新时它是另一个进程，
# 能停能起，但要是新版本起不来，得有人把旧的换回去，那正是这里做的事。
#
# 流程：查版本 → 备份数据 → 二次确认 → 停服务 → 更新 → 起服务 → 验活
#      任何一步失败都回滚到旧版本并把服务拉起来。

set -eu

# 脚本可能被放在 scripts/ 下，也可能被拷到安装目录里直接跑，
# 两种情况都要能定位到安装目录。
if APP_DIR=$(cd "$(dirname "$0")/.." 2>/dev/null && pwd); then
    :
else
    APP_DIR=$(pwd)
fi
BIN="$APP_DIR/moecard"
SERVICE=moecard

err()  { printf '\033[31m%s\033[0m\n' "$*" >&2; }
ok()   { printf '\033[32m%s\033[0m\n' "$*"; }
dim()  { printf '\033[90m%s\033[0m\n' "$*"; }
warn() { printf '\033[33m%s\033[0m\n' "$*"; }
die()  { err "错误: $*"; exit 1; }

[ -x "$BIN" ] || die "在 $APP_DIR 找不到可执行的 moecard
请在安装目录里运行，或者 cd 到安装目录后执行 sh scripts/upgrade.sh"

# ---------------------------------------------------------------- 确认提示
#
# 非交互环境（管道、CI、cron）一律当成"没确认"。升级会替换可执行文件、
# 重启线上服务，在没人看着的时候悄悄做完，比不升级危险得多。
confirm() {
    if [ ! -t 0 ]; then
        err "检测到非交互环境，已中止。"
        dim "无人值守的升级请显式跑：$BIN -update -y"
        exit 1
    fi
    printf '%s' "$1"
    read -r reply
    case "$reply" in
        y | Y | yes | YES) return 0 ;;
        *) return 1 ;;
    esac
}

# ---------------------------------------------------------------- 服务状态
service_installed() {
    [ -f "/etc/systemd/system/$SERVICE.service" ] && command -v systemctl >/dev/null 2>&1
}

service_active() {
    [ "$(systemctl is-active "$SERVICE" 2>/dev/null)" = active ]
}

# ---------------------------------------------------------------- 开始
echo
ok "MoeCard 升级"
dim "  安装目录 $APP_DIR"
dim "  当前版本 $("$BIN" -version 2>/dev/null | head -1 | sed 's/^MoeCard //')"

if service_installed; then
    dim "  systemd  已安装（当前 $(systemctl is-active "$SERVICE" 2>/dev/null)）"
    WAS_ACTIVE=$(service_active && echo yes || echo no)
else
    dim "  systemd  未安装（手动启动模式）"
    WAS_ACTIVE=no
fi
echo

echo "正在检查更新…"
if ! CHECK=$("$BIN" -check-update 2>&1); then
    err "$CHECK"
    exit 1
fi
echo "$CHECK" | sed 's/^/  /'

echo "$CHECK" | grep -q '已经是最新版本' && { echo; ok "无需升级。"; exit 0; }

# ---------------------------------------------------------------- 备份数据
#
# 先备份再动手。SQLite 用程序自带的一致性快照，直接 cp 一个正在写的库
# 有可能拷到一个残缺的中间状态。
echo
BACKUP=""
if [ -f "$APP_DIR/data/moecard.db" ] && [ -x "$APP_DIR/moecard-migrate" ]; then
    BACKUP="$APP_DIR/data/backup-before-upgrade-$(date +%Y%m%d-%H%M%S).db"
    echo "正在备份数据库"
    # -backup 是布尔开关，路径要用 -backup-to。写成 `-backup <路径>` 的话
    # 路径会被当成位置参数忽略掉，命令照样成功，备份却落在别处。
    # 所以退出码之外还要确认文件真的在 —— 备份这件事不能只信返回值。
    "$APP_DIR/moecard-migrate" -backup -backup-to "$BACKUP" >/dev/null 2>&1 || true
    if [ -s "$BACKUP" ]; then
        ok "  已备份到 $BACKUP"
    else
        warn "  备份失败，继续升级的话出问题就没有退路了"
        if ! confirm "  仍然继续？[y/N] "; then
            echo "已取消。"
            exit 0
        fi
        BACKUP=""
    fi
fi

# ---------------------------------------------------------------- 二次确认
echo
warn "接下来会："
[ "$WAS_ACTIVE" = yes ] && echo "  1. 停止 $SERVICE 服务（站点会短暂无法访问）"
echo "  2. 替换可执行文件（旧版本备份成 moecard.old）"
[ "$WAS_ACTIVE" = yes ] && echo "  3. 重新启动服务并验活，起不来就自动回滚"
echo
confirm "确认升级？[y/N] " || { echo "已取消，什么都没动。"; exit 0; }

# ---------------------------------------------------------------- 执行
echo
if [ "$WAS_ACTIVE" = yes ]; then
    echo "正在停止服务"
    systemctl stop "$SERVICE"
fi

# 回滚：把 .old 换回来再把服务拉起来。
# 走到这一步说明新版本有问题，让站点先跑起来比查清楚原因更要紧。
rollback() {
    err "$1"
    if [ -f "$BIN.old" ]; then
        echo "正在回滚到旧版本"
        mv -f "$BIN.old" "$BIN"
    fi
    if [ "$WAS_ACTIVE" = yes ]; then
        systemctl start "$SERVICE" 2>/dev/null || true
        sleep 3
        if service_active; then
            ok "已回滚，服务恢复运行"
        else
            err "回滚后服务仍未启动，用 journalctl -u $SERVICE -n 30 查看"
        fi
    fi
    [ -n "$BACKUP" ] && dim "数据库备份在 $BACKUP"
    exit 1
}

echo "正在更新"
"$BIN" -update -y || rollback "更新失败"

if [ "$WAS_ACTIVE" = yes ]; then
    echo "正在启动服务"
    systemctl start "$SERVICE" || rollback "服务启动失败"

    echo "正在验活"
    n=0
    while [ "$n" -lt 15 ]; do
        service_active && break
        n=$((n + 1))
        sleep 1
    done
    service_active || rollback "服务起来了又挂了"
fi

# ---------------------------------------------------------------- 完成
echo
ok "✓ 升级完成，当前 $("$BIN" -version 2>/dev/null | head -1 | sed 's/^MoeCard //')"
[ "$WAS_ACTIVE" = yes ] && dim "  服务状态 $(systemctl is-active "$SERVICE")"
[ -f "$BIN.old" ] && dim "  旧版本备份 $BIN.old（留到下次升级）"
[ -n "$BACKUP" ] && dim "  数据库备份 $BACKUP"
echo
dim "确认一切正常后，上面两个备份可以自行删除。"
