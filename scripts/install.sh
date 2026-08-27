#!/usr/bin/env sh
#
# MoeCard 安装脚本
#
#   curl -fsSL https://raw.githubusercontent.com/qixishunzi/MoeCard/main/scripts/install.sh | sh
#
# 做的事：认平台 → 下最新的包 → 校验 sha256 → 解压到当前目录。
# 不动 systemd、不改防火墙、不写任何系统目录 —— 那些是你自己该决定的事，
# 一个从管道里跑起来的脚本不该替你做主。
#
# 用 sh 而不是 bash：Alpine 之类的镜像里没有 bash。

set -eu

REPO="${MOECARD_REPO:-qixishunzi/MoeCard}"
DIR="${MOECARD_DIR:-./moecard}"
VERSION="${MOECARD_VERSION:-latest}"

err() { printf '\033[31m%s\033[0m\n' "$*" >&2; }
ok() { printf '\033[32m%s\033[0m\n' "$*"; }
dim() { printf '\033[90m%s\033[0m\n' "$*"; }

die() { err "错误: $*"; exit 1; }

need() {
    command -v "$1" >/dev/null 2>&1 || die "需要 $1，请先安装"
}

# ---------------------------------------------------------------- 认平台
detect() {
    os=$(uname -s | tr '[:upper:]' '[:lower:]')
    case "$os" in
        linux)   OS=linux ;;
        darwin)  OS=darwin ;;
        # Git Bash / MSYS 下 uname 是这些值。Windows 用户其实该直接下 zip，
        # 但既然人已经在这儿了，就让脚本能跑完。
        mingw*|msys*|cygwin*) OS=windows ;;
        *) die "不支持的系统: $os（支持 Linux / macOS / Windows）" ;;
    esac

    arch=$(uname -m)
    case "$arch" in
        x86_64|amd64)   ARCH=amd64 ;;
        aarch64|arm64)  ARCH=arm64 ;;
        *) die "不支持的架构: $arch（支持 x86_64 / arm64）" ;;
    esac

    # 用 if 而不是 `[ ... ] && ...`：
    # set -e 下，条件为假时这个复合命令返回非零，而它又是函数的最后一条语句，
    # 于是整个函数返回 1，脚本当场退出。Windows 分支恰好总为真，所以在
    # Windows 上测不出来 —— Linux 和 macOS 用户会在这一行静默中止。
    if [ "$OS" = windows ]; then
        EXT=zip
    else
        EXT=tar.gz
    fi
}

# ---------------------------------------------------------------- 取版本
resolve_version() {
    if [ "$VERSION" != latest ]; then
        VERSION="${VERSION#v}"
        return
    fi
    dim "正在查询最新版本…"
    api="https://api.github.com/repos/$REPO/releases/latest"
    tag=$(fetch "$api" | grep -m1 '"tag_name"' | sed 's/.*"tag_name" *: *"\([^"]*\)".*/\1/')
    [ -n "$tag" ] || die "查不到最新版本，检查网络或者手动指定 MOECARD_VERSION=1.0.0"
    VERSION="${tag#v}"
}

fetch() {
    if command -v curl >/dev/null 2>&1; then
        curl -fsSL "$1"
    else
        wget -qO- "$1"
    fi
}

download() {
    if command -v curl >/dev/null 2>&1; then
        curl -fsSL -o "$2" "$1"
    else
        wget -qO "$2" "$1"
    fi
}

# ---------------------------------------------------------------- 校验
#
# 这一步不能省。从公网拉一个二进制下来直接跑，至少要确认它和发布者
# 公布的哈希一致。校验文件缺失或对不上一律中止，绝不"那就先装着"。
verify() {
    file="$1"; sums="$2"; name="$3"

    want=$(grep -m1 "[ *]$name\$" "$sums" | awk '{print $1}')
    [ -n "$want" ] || die "校验文件里没有 $name 的记录，已中止"

    if command -v sha256sum >/dev/null 2>&1; then
        got=$(sha256sum "$file" | awk '{print $1}')
    elif command -v shasum >/dev/null 2>&1; then
        got=$(shasum -a 256 "$file" | awk '{print $1}')
    else
        die "找不到 sha256sum 或 shasum，无法校验，已中止"
    fi

    [ "$got" = "$want" ] || die "校验不通过
  期望 $want
  实际 $got
下载可能被截断或篡改，已中止"
    ok "✓ 校验通过"
}

# ---------------------------------------------------------------- 主流程
main() {
    need uname
    command -v curl >/dev/null 2>&1 || command -v wget >/dev/null 2>&1 \
        || die "需要 curl 或 wget"

    detect
    resolve_version

    NAME="moecard_${VERSION}_${OS}_${ARCH}"
    PKG="$NAME.$EXT"
    BASE="https://github.com/$REPO/releases/download/v$VERSION"

    printf '\n'
    ok "MoeCard $VERSION"
    dim "  平台   $OS/$ARCH"
    dim "  安装到 $DIR"
    printf '\n'

    if [ -e "$DIR" ] && [ -n "$(ls -A "$DIR" 2>/dev/null)" ]; then
        # 目录里已经有东西，可能是老版本的数据。让人自己决定怎么办。
        err "$DIR 已存在且非空。"
        dim "升级现有安装请用：cd $DIR && ./moecard -update"
        dim "想装到别处：MOECARD_DIR=/opt/moecard sh install.sh"
        exit 1
    fi

    tmp=$(mktemp -d)
    # 无论成功失败都清干净临时目录，别在 /tmp 里留一堆二进制
    trap 'rm -rf "$tmp"' EXIT INT TERM

    dim "正在下载 $PKG"
    download "$BASE/$PKG" "$tmp/$PKG" || die "下载失败，确认这个版本有 $OS/$ARCH 的包"
    download "$BASE/SHA256SUMS.txt" "$tmp/SHA256SUMS.txt" \
        || die "下载校验文件失败，已中止"

    verify "$tmp/$PKG" "$tmp/SHA256SUMS.txt" "$PKG"

    dim "正在解压"
    if [ "$EXT" = zip ]; then
        need unzip
        unzip -q "$tmp/$PKG" -d "$tmp"
    else
        need tar
        tar -xzf "$tmp/$PKG" -C "$tmp"
    fi

    mkdir -p "$DIR"
    cp -R "$tmp/$NAME/." "$DIR/"
    chmod +x "$DIR/moecard" "$DIR/moecard-migrate" 2>/dev/null || true

    printf '\n'
    ok "✓ 安装完成"
    printf '\n'
    dim "接下来："
    printf '  cd %s\n' "$DIR"
    printf '  cp .env.example .env    # 改掉 JWT_SECRET，设好域名\n'
    printf '  ./moecard\n'
    printf '\n'
    dim "然后浏览器打开 http://你的地址:8080 完成初始化。"
    dim "生产环境请务必看一遍 README 的「正式上线」一节。"
}

main "$@"
