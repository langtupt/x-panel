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

[ "$(id -u)" -eq 0 ] || die "Root access is required. Run this script as root or with sudo."
command -v setquota >/dev/null || die "The quota package is missing. Install it with: apt install quota"

MP="$(findmnt -n -o TARGET --target /home)"
FSTYPE="$(findmnt -n -o FSTYPE --target /home)"
OPTS="$(findmnt -n -o OPTIONS --target /home)"
log "/home is on $MP ($FSTYPE)"

# quotaon -p exits 1 when quota IS on, so capture its output instead of piping it under pipefail.
STATE="$(quotaon -p -u "$MP" 2>/dev/null || true)"
if echo "$OPTS" | grep -qE '(^|,)(usrquota|quota|uquota|usrjquota=[^,]*)(,|$)' && grep -q ' is on$' <<<"$STATE"; then
  log "Quotas are already enabled on $MP."
  exit 0
fi

# Adds a mount option to the fstab entry of $MP.
fstab_add_opt() {
  local opt="$1"
  awk -v mp="$MP" '$1 !~ /^#/ && $2 == mp { found = 1 } END { exit !found }' /etc/fstab \
    || die "No entry for $MP was found in /etc/fstab."
  cp /etc/fstab "/etc/fstab.bak.$(date +%Y%m%d%H%M%S)"
  awk -v mp="$MP" -v opt="$opt" 'BEGIN { OFS = "\t" }
    $1 !~ /^#/ && $2 == mp && ("," $4 ",") !~ ("," opt ",") { $4 = $4 "," opt }
    { print }' /etc/fstab >/etc/fstab.xpanel
  mv /etc/fstab.xpanel /etc/fstab
  log "Added '$opt' to /etc/fstab for $MP (backup: /etc/fstab.bak.*)."
}

case "$FSTYPE" in
  ext4|ext3)
    fstab_add_opt usrquota
    mount -o remount,usrquota "$MP"
    # Filesystems with the ext4 "quota" feature track usage internally;
    # otherwise build aquota.user first.
    quotacheck -cum "$MP" 2>/dev/null || true
    quotaon -u "$MP" 2>/dev/null || true
    findmnt -n -o OPTIONS --target /home | grep -q usrquota || die "Remount failed. Reboot the server and run this script again."
    # quotaon -p exits 1 when quota IS on, so do not pipe it under pipefail.
    state="$(quotaon -p -u "$MP" 2>/dev/null || true)"
    grep -q ' is on$' <<<"$state" || die "User quotas are not enforced by the kernel. Check the quota_v2 module (Ubuntu: linux-modules-extra for the running kernel), then retry."
    log "Quotas enabled on $MP. To reapply limits, edit and save each hosting package in the panel."
    ;;
  xfs)
    if [ "$MP" = "/" ]; then
      # XFS root quotas must be enabled at mount time via the kernel command line.
      if [ -f /etc/default/grub ]; then
        grep -q 'rootflags=uquota' /etc/default/grub \
          || sed -i 's/^GRUB_CMDLINE_LINUX="/GRUB_CMDLINE_LINUX="rootflags=uquota /' /etc/default/grub
        update-grub
      else
        die "Cannot add rootflags=uquota: /etc/default/grub was not found."
      fi
    fi
    fstab_add_opt uquota
    log "XFS: REBOOT is required to activate quotas."
    ;;
  *)
    die "Unsupported filesystem: $FSTYPE. Only ext4 and XFS are supported."
    ;;
esac
