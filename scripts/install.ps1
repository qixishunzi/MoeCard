# MoeCard Windows 安装脚本
#
#   irm https://raw.githubusercontent.com/qixishunzi/MoeCard/main/scripts/install.ps1 | iex
#
# 做的事：认架构 → 下最新的包 → 校验 sha256 → 解压到当前目录下的 moecard\
# 不注册服务、不改防火墙、不写注册表。

$ErrorActionPreference = 'Stop'

$Repo    = if ($env:MOECARD_REPO)    { $env:MOECARD_REPO }    else { 'qixishunzi/MoeCard' }
$Dir     = if ($env:MOECARD_DIR)     { $env:MOECARD_DIR }     else { '.\moecard' }
$Version = if ($env:MOECARD_VERSION) { $env:MOECARD_VERSION } else { 'latest' }

function Die($msg) { Write-Host "错误: $msg" -ForegroundColor Red; exit 1 }
function Dim($msg) { Write-Host $msg -ForegroundColor DarkGray }

# ---- 认架构 ----
$arch = switch ($env:PROCESSOR_ARCHITECTURE) {
    'AMD64' { 'amd64' }
    'ARM64' { 'arm64' }
    default { Die "不支持的架构: $($env:PROCESSOR_ARCHITECTURE)（支持 AMD64 / ARM64）" }
}

# ---- 取版本 ----
if ($Version -eq 'latest') {
    Dim '正在查询最新版本…'
    try {
        $rel = Invoke-RestMethod "https://api.github.com/repos/$Repo/releases/latest" `
            -Headers @{ 'User-Agent' = 'MoeCard-Installer' }
    } catch {
        Die "查不到最新版本，检查网络或者手动指定 `$env:MOECARD_VERSION='1.0.0'"
    }
    $Version = $rel.tag_name -replace '^v', ''
}
$Version = $Version -replace '^v', ''

$name = "moecard_${Version}_windows_${arch}"
$pkg  = "$name.zip"
$base = "https://github.com/$Repo/releases/download/v$Version"

Write-Host ''
Write-Host "MoeCard $Version" -ForegroundColor Green
Dim "  平台   windows/$arch"
Dim "  安装到 $Dir"
Write-Host ''

if ((Test-Path $Dir) -and (Get-ChildItem $Dir -Force -ErrorAction SilentlyContinue)) {
    # 目录里已经有东西，可能是老版本的数据，让人自己决定怎么办
    Write-Host "$Dir 已存在且非空。" -ForegroundColor Red
    Dim "升级现有安装请用：cd $Dir; .\moecard.exe -update"
    Dim "想装到别处：`$env:MOECARD_DIR='D:\moecard'"
    exit 1
}

$tmp = Join-Path $env:TEMP ("moecard-" + [guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $tmp -Force | Out-Null
try {
    Dim "正在下载 $pkg"
    try {
        Invoke-WebRequest "$base/$pkg" -OutFile "$tmp\$pkg" -UseBasicParsing
    } catch {
        Die "下载失败，确认这个版本有 windows/$arch 的包"
    }
    try {
        Invoke-WebRequest "$base/SHA256SUMS.txt" -OutFile "$tmp\SHA256SUMS.txt" -UseBasicParsing
    } catch {
        Die '下载校验文件失败，已中止'
    }

    # ---- 校验 ----
    # 从公网拉一个 exe 下来直接跑，至少要确认它和发布者公布的哈希一致。
    # 校验文件缺失或对不上一律中止，绝不「那就先装着」。
    Dim '正在校验完整性'
    $line = Select-String -Path "$tmp\SHA256SUMS.txt" -Pattern "[ *]$([regex]::Escape($pkg))$" |
            Select-Object -First 1
    if (-not $line) { Die "校验文件里没有 $pkg 的记录，已中止" }
    $want = ($line.Line -split '\s+')[0].ToLower()
    $got  = (Get-FileHash "$tmp\$pkg" -Algorithm SHA256).Hash.ToLower()
    if ($got -ne $want) {
        Die "校验不通过`n  期望 $want`n  实际 $got`n下载可能被截断或篡改，已中止"
    }
    Write-Host '✓ 校验通过' -ForegroundColor Green

    Dim '正在解压'
    Expand-Archive -Path "$tmp\$pkg" -DestinationPath $tmp -Force

    New-Item -ItemType Directory -Path $Dir -Force | Out-Null
    Copy-Item "$tmp\$name\*" -Destination $Dir -Recurse -Force
} finally {
    Remove-Item $tmp -Recurse -Force -ErrorAction SilentlyContinue
}

Write-Host ''
Write-Host '✓ 安装完成' -ForegroundColor Green
Write-Host ''
Dim '接下来：'
Write-Host "  cd $Dir"
Write-Host '  copy .env.example .env    # 改掉 JWT_SECRET，设好域名'
Write-Host '  .\moecard.exe'
Write-Host ''
Dim '然后浏览器打开 http://127.0.0.1:8080 完成初始化。'
Dim '生产环境请务必看一遍 README 的「正式上线」一节。'
