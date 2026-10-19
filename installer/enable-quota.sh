#!/usr/bin/env bash
# Enable Linux user disk quotas on the filesystem that holds /home, so
# X-Panel package disk limits are enforced.
#   ext4: enabled live (fstab + remount + quotaon), no reboot.
#   xfs : fstab / kernel cmdline updated, a reboot is required.
# /etc/fstab is backed up before any change.
set -euo pipefail
export LC_ALL=C

die() { printf '\033[1;31mxx\033[0m %s\n' "$*" >&2; exit 1; }
log() { printf '\033[1;34m==>\033[0m %s\n' "$*"; }

[ "$(id -u)" -eq 0 ] || die "Cần chạy bằng root (sudo)."
command -v setquota >/dev/null || die "Chưa cài gói quota (apt install quota)."

MP="$(findmnt -n -o TARGET --target /home)"
FSTYPE="$(findmnt -n -o FSTYPE --target /home)"
OPTS="$(findmnt -n -o OPTIONS --target /home)"
log "/home nằm trên $MP ($FSTYPE)"

# quotaon -p exits 1 when quota IS on, so capture its output instead of piping it under pipefail.
STATE="$(quotaon -p -u "$MP" 2>/dev/null || true)"
if echo "$OPTS" | grep -qE '(^|,)(usrquota|quota|uquota|usrjquota=[^,]*)(,|$)' && grep -q ' is on$' <<<"$STATE"; then
  log "Quota đã được bật trên $MP."
  exit 0
fi

# Adds a mount option to the fstab entry of $MP.
fstab_add_opt() {
  local opt="$1"
  awk -v mp="$MP" '$1 !~ /^#/ && $2 == mp { found = 1 } END { exit !found }' /etc/fstab \
    || die "Không tìm thấy dòng cho $MP trong /etc/fstab."
  cp /etc/fstab "/etc/fstab.bak.$(date +%Y%m%d%H%M%S)"
  awk -v mp="$MP" -v opt="$opt" 'BEGIN { OFS = "\t" }
    $1 !~ /^#/ && $2 == mp && ("," $4 ",") !~ ("," opt ",") { $4 = $4 "," opt }
    { print }' /etc/fstab >/etc/fstab.xpanel
  mv /etc/fstab.xpanel /etc/fstab
  log "Đã thêm '$opt' vào /etc/fstab cho $MP (bản sao lưu: /etc/fstab.bak.*)."
}

case "$FSTYPE" in
  ext4|ext3)
    fstab_add_opt usrquota
    mount -o remount,usrquota "$MP"
    # Filesystems with the ext4 "quota" feature track usage internally;
    # otherwise build aquota.user first.
    quotacheck -cum "$MP" 2>/dev/null || true
    quotaon -u "$MP" 2>/dev/null || true
    findmnt -n -o OPTIONS --target /home | grep -q usrquota || die "Remount thất bại, hãy reboot rồi chạy lại."
    # quotaon -p exits 1 when quota IS on, so do not pipe it under pipefail.
    state="$(quotaon -p -u "$MP" 2>/dev/null || true)"
    grep -q ' is on$' <<<"$state" || die "Kernel chưa thực thi user quota. Kiểm tra module quota_v2 (Ubuntu: linux-modules-extra cho kernel hiện tại), rồi chạy lại."
    log "Đã bật quota trên $MP. Áp lại giới hạn: sửa và lưu lại từng gói trong panel."
    ;;
  xfs)
    if [ "$MP" = "/" ]; then
      # XFS root quotas must be enabled at mount time via the kernel command line.
      if [ -f /etc/default/grub ]; then
        grep -q 'rootflags=uquota' /etc/default/grub \
          || sed -i 's/^GRUB_CMDLINE_LINUX="/GRUB_CMDLINE_LINUX="rootflags=uquota /' /etc/default/grub
        update-grub
      else
        die "Không tìm thấy /etc/default/grub để thêm rootflags=uquota."
      fi
    fi
    fstab_add_opt uquota
    log "XFS: cần REBOOT để quota có hiệu lực."
    ;;
  *)
    die "Filesystem $FSTYPE chưa được hỗ trợ (chỉ ext4, xfs)."
    ;;
esac
