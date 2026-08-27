#!/usr/bin/env sh
#
# MoeCard 卸载脚本
#
#   cd /opt/moecard && sudo sh scripts/uninstall.sh
#
# 分两层：撤服务是可逆的，删数据不可逆，所以确认方式不一样。
#
#   撤服务、删程序  →  y/N 确认
#   删数据          →  必须**手打安装目录的完整路径**
#
# 为什么删数据不能只按 y：那里面是订单记录和还没卖出去的卡密，
# 删了就是真没了。y/N 太容易顺手敲下去 —— 前一个问题刚敲完 y，
# 手还没离开键盘。逼你把路径打一遍，至少能确认删的是哪个目录。

set -eu

# 脚本可能被放在 scripts/ 下，也可能被拷到安装目录里直接跑，
# 两种情况都要能定位到安装目录。
if APP_DIR=$(cd "$(dirname "$0")/.." 2>/dev/null && pwd); then
    :
else
    APP_DIR=$(pwd)
fi
SERVICE=moecard
SERVICE_USER=moecard
UNIT=/etc/systemd/system/$SERVICE.service

err()  { printf '\033[31m%s\033[0m\n' "$*" >&2; }
ok()   { printf '\033[32m%s\033[0m\n' "$*"; }
dim()  { printf '\033[90m%s\033[0m\n' "$*"; }
warn() { printf '\033[33m%s\033[0m\n' "$*"; }

require_tty() {
    if [ ! -t 0 ]; then
        err "检测到非交互环境，已中止。"
        dim "卸载会删东西，不接受无人值守执行。"
        exit 1
    fi
}

confirm() {
    require_tty
    printf '%s' "$1"
    read -r reply
    case "$reply" in y | Y | yes | YES) return 0 ;; *) return 1 ;; esac
}

# confirm_exact 要求原样打出某个字符串才算通过。
# 用在不可逆的操作上 —— y/N 挡不住手快。
confirm_exact() {
    require_tty
    printf '%s' "$1"
    read -r reply
    [ "$reply" = "$2" ]
}

# ---------------------------------------------------------------- 摸清现状
echo
warn "MoeCard 卸载"
dim "  安装目录 $APP_DIR"

HAS_SERVICE=no
[ -f "$UNIT" ] && command -v systemctl >/dev/null 2>&1 && HAS_SERVICE=yes
if [ "$HAS_SERVICE" = yes ]; then
    dim "  systemd  已安装（$(systemctl is-active "$SERVICE" 2>/dev/null)）"
else
    dim "  systemd  未安装"
fi

DB="$APP_DIR/data/moecard.db"
if [ -f "$DB" ]; then
    dim "  数据库   $DB（$(du -h "$DB" 2>/dev/null | cut -f1)）"
else
    dim "  数据库   没找到"
fi

UPLOADS="$APP_DIR/storage/uploads"
if [ -d "$UPLOADS" ]; then
    dim "  上传文件 $UPLOADS（$(du -sh "$UPLOADS" 2>/dev/null | cut -f1)）"
fi

# ---------------------------------------------------------------- 第一层确认
echo
echo "将要执行："
[ "$HAS_SERVICE" = yes ] && echo "  · 停止并移除 systemd 服务"
echo "  · 之后询问是否删除数据与程序文件"
echo
confirm "继续吗？[y/N] " || { echo "已取消，什么都没动。"; exit 0; }

# ---------------------------------------------------------------- 撤服务
if [ "$HAS_SERVICE" = yes ]; then
    echo
    echo "正在停止并移除服务"
    systemctl stop "$SERVICE" 2>/dev/null || true
    systemctl disable "$SERVICE" 2>/dev/null || true
    rm -f "$UNIT"
    systemctl daemon-reload 2>/dev/null || true
    ok "  服务已移除"
fi

# ---------------------------------------------------------------- 备份
#
# 在问"要不要删"之前先问"要不要留个备份"。顺序反过来的话，
# 人已经打完删除确认了，再问备份就是多余的一步。
BACKUP=""
if [ -f "$DB" ]; then
    echo
    if confirm "先把数据库备份出来吗？（强烈建议）[y/N] "; then
        BACKUP="$HOME/moecard-backup-$(date +%Y%m%d-%H%M%S).db"

        # 备份必须落在安装目录**外面** —— 下面马上就要 rm -rf 安装目录，
        # 放在里面等于没备份。$HOME 是安全的。
        #
        # -backup 是布尔开关，路径要用 -backup-to。写成 `-backup <路径>`
        # 的话路径会被当位置参数忽略，命令照样成功，备份落到安装目录下的
        # backups/，然后被一起删掉 —— 用户以为有退路，实际什么都没剩。
        if [ -x "$APP_DIR/moecard-migrate" ]; then
            "$APP_DIR/moecard-migrate" -backup -backup-to "$BACKUP" >/dev/null 2>&1 || true
        fi
        # 只信文件是否真的存在，不信退出码
        if [ ! -s "$BACKUP" ]; then
            cp "$DB" "$BACKUP" 2>/dev/null || true
            [ -s "$BACKUP" ] && dim "  （一致性快照失败，改用直接复制；服务已停，通常没问题）"
        fi

        if [ -s "$BACKUP" ]; then
            ok "  已备份到 $BACKUP（$(du -h "$BACKUP" | cut -f1)）"
        else
            err "  备份失败，没有生成任何文件"
            BACKUP=""
            if ! confirm "  没有备份也要继续吗？[y/N] "; then
                echo "已取消。服务已移除，数据完好。"
                exit 0
            fi
        fi
    fi
fi

# ---------------------------------------------------------------- 第二层确认
echo
warn "接下来是不可逆操作。"
echo
echo "删除 $APP_DIR 会连同以下内容一起消失："
[ -f "$DB" ]      && echo "  · 全部订单记录与未售出的卡密"
[ -d "$UPLOADS" ] && echo "  · 全部上传的图片"
echo "  · .env 里的配置（含支付渠道密钥）"
echo
dim "只想撤掉开机自启、保留数据的话，到这里 Ctrl-C 就行 —— 服务已经移除了。"
echo

if confirm_exact "确认删除请完整输入安装目录路径：
  $APP_DIR
> " "$APP_DIR"; then
    echo
    echo "正在删除 $APP_DIR"
    # 先切出去，否则删的是自己脚下的目录
    cd /
    rm -rf "$APP_DIR"
    ok "  已删除"
    DELETED=yes
else
    echo
    ok "路径不匹配，已保留所有文件。"
    dim "服务已经移除，程序和数据都还在 $APP_DIR"
    DELETED=no
fi

# ---------------------------------------------------------------- 用户
if id "$SERVICE_USER" >/dev/null 2>&1; then
    echo
    if confirm "顺便删掉 $SERVICE_USER 这个系统用户吗？[y/N] "; then
        if userdel "$SERVICE_USER" 2>/dev/null; then
            ok "  已删除"
        else
            err "  删除失败（可能还有进程在用）"
        fi
    else
        dim "  保留。以后要删：userdel $SERVICE_USER"
    fi
fi

# ---------------------------------------------------------------- 收尾
echo
ok "✓ 卸载完成"
[ -n "$BACKUP" ] && dim "  数据库备份留在 $BACKUP"
[ "${DELETED:-no}" = no ] && dim "  程序文件仍在 $APP_DIR，确认不要了自行删除"
echo
