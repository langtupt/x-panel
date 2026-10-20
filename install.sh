#!/usr/bin/env bash
# X-Panel — cài đặt / nâng cấp bằng 1 lệnh (đăng nhập root rồi dán):
#
#   curl -fsSL https://raw.githubusercontent.com/langtupt/x-panel/main/install.sh | bash
#
# Chưa có curl (bản cài tối giản):
#   apt-get -o APT::Update::Error-Mode=any update && apt-get install -y curl ca-certificates && curl -fSL -o /tmp/xpanel-install.sh https://raw.githubusercontent.com/langtupt/x-panel/main/install.sh && bash /tmp/xpanel-install.sh
#
# Tuỳ chọn (biến môi trường, đặt trước "bash"):
#   ADMIN_PASSWORD='...'   mật khẩu admin (mặc định: tạo ngẫu nhiên và hiện ở cuối)
#   XPANEL_VERSION=v1.1.0  cài một phiên bản cụ thể (mặc định: mới nhất)
#   XPANEL_FORCE=1         bỏ qua kiểm tra "VPS đã có web server/panel khác"
#
# Tải bản phát hành cho CPU của máy từ GitHub, kiểm tra SHA-256, cài đặt, bật
# quota và tối ưu VPS. Chạy lại = nâng cấp (giữ nguyên dữ liệu và mật khẩu).
set -euo pipefail

# Everything runs inside main(), called on the last line: with "curl | bash" a
# truncated download then does nothing instead of running half a script.
main() {

REPO="${XPANEL_REPO:-langtupt/x-panel}"
XP_VERSION="${XPANEL_VERSION:-latest}"
LOG=/var/log/xpanel-install.log

say()  { printf '\033[1;34m==>\033[0m %s\n' "$*"; }
warn() { printf '\033[1;33m!!\033[0m %s\n' "$*"; }
die()  { printf '\033[1;31mxx %s\033[0m\n' "$*" >&2; exit 1; }

# ---------------------------------------------------------------- checks
[ "$(id -u)" -eq 0 ] || die "Cần quyền root. Đăng nhập bằng tài khoản root (hoặc chạy: sudo -i) rồi dán lại lệnh."
[ -r /etc/os-release ] || die "Không nhận diện được hệ điều hành."
# Read in a subshell: os-release defines VERSION, NAME… which must not
# overwrite this script's variables.
OS_ID=$(. /etc/os-release && echo "${ID:-}")
OS_VER=$(. /etc/os-release && echo "${VERSION_ID:-}")
PRETTY_NAME=$(. /etc/os-release && echo "${PRETTY_NAME:-$OS_ID $OS_VER}")
case "$OS_ID:$OS_VER" in
  debian:12*|debian:13*|ubuntu:22.04|ubuntu:24.04) ;;
  *) die "Hệ điều hành chưa hỗ trợ: ${PRETTY_NAME:-không rõ}. X-Panel cần Debian 12/13 hoặc Ubuntu 22.04/24.04 (bản mới cài)." ;;
esac
case "$(uname -m)" in
  x86_64|amd64) ARCH=amd64 ;;
  aarch64|arm64) ARCH=arm64 ;;
  *) die "Kiến trúc CPU chưa hỗ trợ: $(uname -m) (cần x86_64 hoặc arm64)." ;;
esac

MEM_MB=$(awk '/^MemTotal:/ {print int($2/1024)}' /proc/meminfo)
DISK_MB=$(df -Pm / | awk 'NR==2 {print $4}')
[ "$MEM_MB" -ge 700 ] || die "RAM quá ít (${MEM_MB} MB). Cần tối thiểu 1 GB."
[ "$DISK_MB" -ge 3000 ] || die "Ổ đĩa còn trống quá ít (${DISK_MB} MB). Cần tối thiểu 5 GB."
[ "$MEM_MB" -ge 1500 ] || warn "RAM ${MEM_MB} MB: chạy được nhưng nên dùng VPS từ 2 GB nếu bật email và nhiều website."

UPGRADE=0
[ -f /etc/xpanel/xpanel.env ] && UPGRADE=1
# A fresh install takes over ports 80/443 and the web server: refuse to run
# on a machine that already serves sites with something else.
if [ "$UPGRADE" = 0 ] && [ "${XPANEL_FORCE:-0}" != 1 ]; then
  for svc in apache2 httpd lsws litespeed caddy; do
    if systemctl is-active -q "$svc" 2>/dev/null; then
      die "VPS đang chạy $svc. X-Panel cần một VPS mới cài (nó quản lý nginx, PHP, MariaDB, mail). Cài lại hệ điều hành rồi chạy lệnh, hoặc đặt XPANEL_FORCE=1 nếu biết mình đang làm gì."
    fi
  done
  for dir in /usr/local/cpanel /www/server/panel /usr/local/directadmin /usr/local/hestia /usr/local/vesta /usr/local/CyberCP /usr/local/fastpanel2 /opt/fastpanel2; do
    [ -e "$dir" ] && die "Phát hiện panel khác ($dir). Hãy cài X-Panel trên VPS mới cài hệ điều hành."
  done
fi

# ---------------------------------------------------------------- download
export DEBIAN_FRONTEND=noninteractive
getent ahosts github.com >/dev/null 2>&1 \
  || die "Không phân giải được github.com. Kiểm tra kết nối mạng và DNS của VPS (getent hosts github.com). Xem mục khắc phục DNS trong README; script không tự thay DNS hệ thống."
if ! command -v curl >/dev/null || ! command -v sha256sum >/dev/null; then
  say "Cài curl"
  apt-get -o Acquire::Retries=3 -o APT::Update::Error-Mode=any update -q </dev/null \
    || die "Không cập nhật được APT. Kiểm tra lỗi DNS/kết nối hoặc kho gói ở trên."
  apt-get -o Acquire::Retries=3 install -y -q curl ca-certificates coreutils </dev/null \
    || die "Không cài được curl và chứng chỉ CA. Kiểm tra kết nối đến kho gói ở trên."
fi

if [ "$XP_VERSION" = latest ]; then
  BASE="https://github.com/$REPO/releases/latest/download"
else
  BASE="https://github.com/$REPO/releases/download/$XP_VERSION"
fi
FILE="xpanel-linux-$ARCH.tar.gz"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

echo
say "X-Panel — ${PRETTY_NAME}, ${ARCH}, RAM ${MEM_MB} MB$( [ "$UPGRADE" = 1 ] && echo ", NÂNG CẤP bản đang cài")"
say "Tải bản phát hành ($XP_VERSION)…"
curl -fsSL --retry 3 -o "$TMP/$FILE" "$BASE/$FILE" || die "Không tải được $BASE/$FILE (kiểm tra kết nối Internet của VPS)."
curl -fsSL --retry 3 -o "$TMP/$FILE.sha256" "$BASE/$FILE.sha256" || die "Không tải được tệp SHA-256."
(cd "$TMP" && sha256sum -c --quiet "$FILE.sha256") || die "SHA-256 không khớp: tệp tải về bị hỏng, hãy chạy lại lệnh."
tar -C "$TMP" -xzf "$TMP/$FILE"

# ---------------------------------------------------------------- install
say "Đang cài đặt, mất khoảng 3–10 phút tuỳ VPS (log: $LOG)…"
cd "$TMP/xpanel-linux-$ARCH"
set +e
ADMIN_PASSWORD="${ADMIN_PASSWORD:-}" XPANEL_ENABLE_QUOTA=1 ./install.sh </dev/null 2>&1 | tee "$LOG"
rc=${PIPESTATUS[0]}
set -e
chmod 600 "$LOG"
# The log must not keep the admin password shown at the end.
sed -i 's/^\(  Mật khẩu  : \).*$/\1(đã hiển thị trên màn hình)/' "$LOG" 2>/dev/null || true
if [ "$rc" != 0 ]; then
  echo
  die "Cài đặt thất bại (mã $rc). Xem lỗi ở trên hoặc trong $LOG; chạy lại lệnh sau khi khắc phục."
fi
}

main "$@"
