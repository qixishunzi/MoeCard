#!/usr/bin/env sh
#
# install.sh 的自检。CI 里跑，也可以本地手动跑：
#
#   sh scripts/test-install.sh
#
# 为什么需要它：install.sh 是从管道里直接执行的，出错就是「用户装不上」。
# 而它最容易出的错——`set -e` 配上 `cmd && cmd` 在条件为假时静默退出——
# 只在某一个分支上暴露。真出过一次：
#
#     EXT=tar.gz
#     [ "$OS" = windows ] && EXT=zip     # 非 Windows 时函数返回 1，脚本当场死
#
# Windows 上这个判断恒真所以永远测不出来，Linux 和 macOS 全军覆没。
# 下面把三个平台的 detect() 都跑一遍，就是为了不再重演。

set -eu

SCRIPT="${1:-scripts/install.sh}"
fail=0

ok()   { printf '\033[32mok  \033[0m %s\n' "$*"; }
bad()  { printf '\033[31mFAIL\033[0m %s\n' "$*"; fail=$((fail + 1)); }

# ---------------------------------------------------------------- 语法
if sh -n "$SCRIPT"; then
    ok "语法检查通过"
else
    bad "语法检查未通过"
fi

# ---------------------------------------------------------------- 只取函数定义
# 把 main 的调用去掉，只留函数，这样可以单独调用而不会真的去下载。
defs=$(sed '/^main "\$@"/d' "$SCRIPT")

# ---------------------------------------------------------------- detect()
# 伪造 uname，三个平台各跑一遍。这是踩过坑的那个函数。
for case in "Linux x86_64 linux amd64 tar.gz" \
            "Linux aarch64 linux arm64 tar.gz" \
            "Darwin arm64 darwin arm64 tar.gz" \
            "Darwin x86_64 darwin amd64 tar.gz" \
            "MINGW64_NT-10.0 x86_64 windows amd64 zip"
do
    # shellcheck disable=SC2086  # 就是要靠拆词把一行拆成多个参数
    set -- $case
    sysname=$1; machine=$2; want_os=$3; want_arch=$4; want_ext=$5

    out=$(sh -c "
        uname() { [ \"\$1\" = -s ] && echo '$sysname' || echo '$machine'; }
        $defs
        detect
        echo \"\$OS \$ARCH \$EXT\"
    " 2>&1) && rc=0 || rc=$?

    if [ "$rc" -ne 0 ]; then
        bad "detect($sysname/$machine) 退出码 $rc —— 多半又是 set -e 撞上 && 短路"
    elif [ "$out" = "$want_os $want_arch $want_ext" ]; then
        ok "detect($sysname/$machine) -> $out"
    else
        bad "detect($sysname/$machine) 得到 '$out'，期望 '$want_os $want_arch $want_ext'"
    fi
done

# 不认识的平台必须明确报错，而不是往下走
for case in "FreeBSD x86_64" "Linux mips64"; do
    # shellcheck disable=SC2086  # 就是要靠拆词把一行拆成多个参数
    set -- $case
    if sh -c "
        uname() { [ \"\$1\" = -s ] && echo '$1' || echo '$2'; }
        $defs
        detect
    " >/dev/null 2>&1; then
        bad "detect($1/$2) 应该拒绝，却通过了"
    else
        ok "detect($1/$2) 被拒绝"
    fi
done

# ---------------------------------------------------------------- verify()
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT INT TERM
cd "$tmp"
printf 'fake package' > pkg.tar.gz
sum=$(sha256sum pkg.tar.gz 2>/dev/null | awk '{print $1}' \
      || shasum -a 256 pkg.tar.gz | awk '{print $1}')

run_verify() {
    sh -c "$defs
verify \"\$@\"" _ "$@" >/dev/null 2>&1
}

# expect_pass / expect_fail 用 if 而不是 `A && B || C`：
# 后者在 B 返回非零时还会接着跑 C，判定就反了。这个脚本存在的原因
# 恰好是一个 && 短路的 bug，别在这儿重蹈覆辙。
expect_pass() {
    label=$1; shift
    if run_verify "$@"; then ok "$label"; else bad "$label"; fi
}
expect_fail() {
    label=$1; shift
    if run_verify "$@"; then bad "$label（应当被拒却通过了）"; else ok "$label"; fi
}

printf '%s  pkg.tar.gz\n' "$sum" > good.txt
expect_pass "verify 正确的包通过" pkg.tar.gz good.txt pkg.tar.gz

printf '%s *pkg.tar.gz\n' "$sum" > star.txt
expect_pass "verify 认二进制模式的星号前缀" pkg.tar.gz star.txt pkg.tar.gz

printf 'deadbeef  pkg.tar.gz\n' > wrong.txt
expect_fail "verify 校验和不符被拒" pkg.tar.gz wrong.txt pkg.tar.gz

printf '%s  别的包.tar.gz\n' "$sum" > other.txt
expect_fail "verify 找不到条目时中止" pkg.tar.gz other.txt pkg.tar.gz

: > empty.txt
expect_fail "verify 空校验文件被拒" pkg.tar.gz empty.txt pkg.tar.gz


# ---------------------------------------------------------------- 结果
echo
if [ "$fail" -eq 0 ]; then
    printf '\033[32m全部通过\033[0m\n'
else
    printf '\033[31m%s 项失败\033[0m\n' "$fail"
    exit 1
fi
