# Hướng dẫn cài đặt X-Panel từ A đến Z

Tài liệu này dành cho người **chưa từng quản trị máy chủ**: bạn chỉ cần một VPS mới, đăng nhập
bằng tài khoản `root` và dán **một lệnh duy nhất**. Mọi thứ còn lại (web server, PHP, database, bảo mật,
tối ưu) X-Panel tự cài.

- [1. Chuẩn bị VPS](#1-chuẩn-bị-vps)
- [2. Mở cổng trên firewall của nhà cung cấp](#2-mở-cổng-trên-firewall-của-nhà-cung-cấp)
- [3. Đăng nhập root vào VPS](#3-đăng-nhập-root-vào-vps)
- [4. Dán lệnh cài đặt](#4-dán-lệnh-cài-đặt)
- [5. Đăng nhập panel lần đầu](#5-đăng-nhập-panel-lần-đầu)
- [6. Các bước nên làm ngay](#6-các-bước-nên-làm-ngay)
- [7. Cập nhật lên phiên bản mới](#7-cập-nhật-lên-phiên-bản-mới)
- [8. Quên mật khẩu admin](#8-quên-mật-khẩu-admin)
- [9. Gỡ cài đặt (xóa sạch)](#9-gỡ-cài-đặt-xóa-sạch)
- [10. Xử lý lỗi thường gặp](#10-xử-lý-lỗi-thường-gặp)

---

## 1. Chuẩn bị VPS

| | Yêu cầu |
|---|---|
| Hệ điều hành | **Ubuntu 24.04** (khuyên dùng), Ubuntu 22.04, **Debian 12** hoặc Debian 13 — bản **mới cài**, 64-bit |
| CPU | x86_64 (amd64) hoặc ARM64 |
| RAM | tối thiểu **1 GB**, khuyên dùng **2 GB** trở lên nếu dùng email |
| Ổ đĩa | tối thiểu 5 GB trống, khuyên dùng 20 GB+ |
| Mạng | IPv4 công khai, VPS truy cập được Internet |

> **Quan trọng:** dùng VPS **mới cài hệ điều hành**. Đừng cài lên máy đang chạy cPanel, aaPanel,
> DirectAdmin, Apache… — trình cài đặt sẽ phát hiện và dừng lại để không làm hỏng máy.

Khi tạo VPS ở nhà cung cấp, chọn hệ điều hành **Ubuntu 24.04 LTS** và ghi lại **địa chỉ IP** cùng
**mật khẩu root** (nhà cung cấp gửi qua email hoặc hiển thị trên trang quản lý).

## 2. Mở cổng trên firewall của nhà cung cấp

Nhiều nhà cung cấp (AWS, Google Cloud, Azure, Vultr, Oracle…) có firewall riêng ngoài VPS.
Hãy mở các cổng TCP sau (Inbound):

| Cổng | Dùng cho | Bắt buộc |
|---|---|---|
| 22 | SSH / SFTP | ✅ |
| 80, 443 | Website (HTTP/HTTPS) | ✅ |
| **8888** | **Trang quản trị X-Panel** | ✅ |
| 8890 | phpMyAdmin / Adminer | nếu dùng |
| 21, 30000–30100 | FTP | nếu dùng FTP |
| 25, 465, 587, 993, 995, 110, 143 | Email (SMTP, IMAP, POP3) | nếu dùng email |
| 2096 | Webmail (Roundcube) | nếu dùng email |

Firewall **bên trong** VPS (ufw) X-Panel tự mở khi cài. Lưu ý: một số nhà cung cấp chặn cổng 25 (gửi
email) mặc định — hãy gửi yêu cầu mở nếu muốn gửi thư ra ngoài.

## 3. Đăng nhập root vào VPS

**Windows 10/11:** mở **PowerShell** (bấm Start, gõ `PowerShell`) rồi gõ:

```powershell
ssh root@IP-CỦA-VPS
```

Lần đầu hỏi `Are you sure you want to continue connecting` thì gõ `yes`, rồi nhập mật khẩu root
(khi gõ mật khẩu màn hình **không hiện ký tự** — cứ gõ rồi Enter). Có thể dùng **PuTTY** hoặc
**Termius** nếu quen.

**macOS / Linux:** mở **Terminal** và gõ `ssh root@IP-CỦA-VPS`.

**Không có quyền root trực tiếp** (ví dụ AWS đăng nhập bằng `ubuntu`): sau khi đăng nhập gõ
`sudo -i` để chuyển sang root.

Đăng nhập thành công khi dấu nhắc có dạng `root@ten-may:~#`.

## 4. Dán lệnh cài đặt

Sao chép **nguyên dòng** dưới đây, dán vào cửa sổ SSH (PowerShell: chuột phải để dán) và nhấn Enter:

```bash
apt-get -o Acquire::Retries=3 -o APT::Update::Error-Mode=any update && apt-get install -y curl ca-certificates && curl -fSL --retry 3 -o /tmp/xpanel-install.sh https://raw.githubusercontent.com/langtupt/x-panel/main/install.sh && bash /tmp/xpanel-install.sh
```

Muốn tự đặt mật khẩu admin (ít nhất 10 ký tự) thì dùng lệnh này thay cho lệnh trên:

```bash
apt-get -o Acquire::Retries=3 -o APT::Update::Error-Mode=any update && apt-get install -y curl ca-certificates && curl -fSL --retry 3 -o /tmp/xpanel-install.sh https://raw.githubusercontent.com/langtupt/x-panel/main/install.sh && ADMIN_PASSWORD='MatKhauCuaBan123' bash /tmp/xpanel-install.sh
```

Quá trình cài mất khoảng **3–10 phút**. Trình cài đặt tự:

- kiểm tra hệ điều hành, RAM, ổ đĩa và máy có "sạch" không;
- tải bản phát hành mới nhất từ GitHub và **kiểm tra SHA-256**;
- cài nginx, PostgreSQL (cho panel), MariaDB, SFTP/FTP, quota ổ đĩa;
- tối ưu VPS: tạo swap khi RAM thấp, giới hạn log, cấu hình MariaDB theo RAM, gzip, chống dò mật khẩu
  (fail2ban), mở firewall.

Khi xong, màn hình hiện khung như sau — **hãy chụp lại hoặc ghi ra ngay**, vì mật khẩu **chỉ hiện một lần**
và không được lưu trên máy chủ:

```
==================================================================
  X-Panel đã cài xong
==================================================================
  Địa chỉ   : https://123.45.67.89:8888
  Tài khoản : admin
  Mật khẩu  : Xk3pQ9vT2mLw8RzA
==================================================================
```

Nhật ký cài đặt (không chứa mật khẩu) nằm ở `/var/log/xpanel-install.log`.

## 5. Đăng nhập panel lần đầu

1. Mở trình duyệt, vào địa chỉ ở bước trên, ví dụ `https://123.45.67.89:8888`.
2. Trình duyệt cảnh báo **"Kết nối không riêng tư"** — bình thường, vì panel đang dùng chứng chỉ tự ký.
   Chọn **Nâng cao (Advanced) → Tiếp tục truy cập (Proceed)**. Ở bước 6 bạn có thể gắn tên miền và
   chứng chỉ thật để hết cảnh báo.
3. Đăng nhập bằng `admin` và mật khẩu vừa nhận.

## 6. Các bước nên làm ngay

1. **Bật xác thực 2 lớp** cho admin: *Cài đặt tài khoản → Xác thực 2 lớp* (dùng Google Authenticator…).
2. **Cài môi trường chạy website**: *Applications* → cài **PHP** (bản mới nhất), thêm **Node.js**, **Go**,
   **Composer**, **PostgreSQL**, **phpMyAdmin** nếu cần.
3. **Tạo gói dịch vụ**: *Gói dịch vụ* → đặt giới hạn ổ đĩa, số website, database, hộp thư, CPU, RAM.
4. **Tạo tài khoản hosting** (khách hàng): *Tài khoản* → chọn gói. Mỗi tài khoản là một user Linux
   riêng, cách ly hoàn toàn với tài khoản khác.
5. **Tạo website**: *Website* → nhập tên miền (đã trỏ bản ghi **A** về IP VPS), chọn loại
   (HTML, PHP, Laravel, Node.js, Next.js, Go). Sau đó vào *Chứng chỉ SSL* → **Let's Encrypt** miễn phí.
6. **Gắn tên miền cho trang quản trị** (hết cảnh báo chứng chỉ): trỏ ví dụ `panel.tenmien.com` về IP VPS,
   vào *Chứng chỉ SSL* → *Chứng chỉ trang quản trị* → **Cấp Let's Encrypt**, rồi truy cập
   `https://panel.tenmien.com:8888`.
7. **Email** (nếu cần): *Applications* → cài **Mail server** và **Roundcube Webmail** → trang *Email* →
   thêm tên miền, tạo hộp thư, rồi tạo các bản ghi DNS (MX, SPF, DKIM, DMARC) hiển thị ở tab **DNS**.
   Webmail: `https://IP-VPS:2096/webmail/`.
8. **Sao lưu**: mỗi tài khoản hosting tự kết nối **Google Drive của mình** ở trang *Sao lưu*
   (làm theo 3 bước hướng dẫn trên trang), đặt passphrase mã hóa và lịch tự động.

## 7. Cập nhật lên phiên bản mới

Đăng nhập root và **chạy lại đúng lệnh cài đặt** ở bước 4:

```bash
curl -fsSL https://raw.githubusercontent.com/langtupt/x-panel/main/install.sh | bash
```

Trình cài đặt nhận ra máy đã có X-Panel và **nâng cấp**: giữ nguyên website, database, email, tài khoản
và mật khẩu admin. Website không bị gián đoạn (chỉ trang quản trị khởi động lại vài giây).
Cài một phiên bản cụ thể: thêm `XPANEL_VERSION=v1.1.0` trước `bash`. Danh sách phiên bản:
<https://github.com/langtupt/x-panel/releases>.

## 8. Quên mật khẩu admin

Đăng nhập root và chạy lại lệnh cài kèm mật khẩu mới:

```bash
curl -fsSL https://raw.githubusercontent.com/langtupt/x-panel/main/install.sh | ADMIN_PASSWORD='MatKhauMoi12345' bash
```

Mật khẩu admin được đặt lại; mọi thứ khác giữ nguyên.

Admin bật xác thực 2 lớp và **mất điện thoại**: tắt 2FA của admin bằng lệnh sau (chạy với quyền root),
rồi đăng nhập và bật lại:

```bash
cd /tmp && runuser -u postgres -- psql xpanel -c "UPDATE users SET totp_secret=NULL WHERE username='admin'"
```

## 9. Gỡ cài đặt (xóa sạch)

> ⚠️ **Xóa vĩnh viễn** panel **và toàn bộ dữ liệu khách hàng**: website, thư mục home, database,
> email, FTP, cron. Hãy tải các bản backup về trước.

Đăng nhập root và dán:

```bash
curl -fsSL https://raw.githubusercontent.com/langtupt/x-panel/main/uninstall.sh | bash
```

(hoặc gõ `xpanel-uninstall` — lệnh này có sẵn trên máy đã cài X-Panel).
Script liệt kê những gì sẽ bị xóa và yêu cầu gõ **`XOA SACH`** để xác nhận.

Tuỳ chọn (đặt trước `bash`):

| Biến | Tác dụng |
|---|---|
| `XPANEL_KEEP_DATA=1` | chỉ gỡ panel, **giữ lại** website, database, email của khách |
| `XPANEL_PURGE_PACKAGES=1` | gỡ luôn nginx, PHP, MariaDB, PostgreSQL, Postfix, Dovecot, FTP, fail2ban |
| `XPANEL_CONFIRM=XOA-SACH` | không hỏi xác nhận (cho script tự động) |

Ví dụ gỡ sạch hoàn toàn, kể cả phần mềm: `curl -fsSL https://raw.githubusercontent.com/langtupt/x-panel/main/uninstall.sh | XPANEL_PURGE_PACKAGES=1 bash`.

Có thể chạy lệnh purge này sau khi đã gỡ panel bằng lệnh mặc định. Lệnh mặc định
giữ các gói dịch vụ; purge xóa cả gói và dữ liệu dịch vụ, kể cả được cài ngoài panel.
Không kết hợp purge với `XPANEL_KEEP_DATA=1`. Swap, quota, mạng và các file tự tải
trong `/root` vẫn được giữ; muốn VPS trở về trạng thái hệ điều hành mới, hãy cài lại OS.

Cách "sạch" nhất vẫn là **cài lại hệ điều hành** từ trang quản lý của nhà cung cấp VPS.

## 10. Xử lý lỗi thường gặp

| Hiện tượng | Cách xử lý |
|---|---|
| `Temporary failure resolving` | DNS của VPS không hoạt động; xem [hướng dẫn kiểm tra DNS](../README.md#không-tải-được-gói-temporary-failure-resolving), sửa DNS rồi chạy lại. |
| `curl: command not found` | Dùng đúng lệnh ở bước 4 (có phần `apt-get install -y curl` ở đầu). |
| `sudo: command not found` | Bạn đang là root rồi — bỏ chữ `sudo`. |
| `Cần quyền root` | Gõ `sudo -i` rồi dán lại lệnh. |
| `Hệ điều hành chưa hỗ trợ` | Cài lại VPS bằng Ubuntu 24.04 / 22.04 hoặc Debian 12 / 13. |
| `VPS đang chạy apache2` / `Phát hiện panel khác` | Cài lại hệ điều hành mới rồi chạy lệnh. |
| Không mở được `https://IP:8888` | Mở cổng 8888 ở firewall của nhà cung cấp (bước 2); kiểm tra `systemctl status xpanel-api nginx`. |
| Trình duyệt báo "không an toàn" | Bình thường với chứng chỉ tự ký — chọn *Advanced → Proceed*, hoặc gắn tên miền (bước 6.6). |
| Let's Encrypt báo lỗi | Tên miền phải trỏ bản ghi A đúng IP VPS, cổng 80 phải mở; đợi DNS cập nhật (5–30 phút). |
| Không gửi được email ra ngoài | Nhà cung cấp chặn cổng 25 — yêu cầu mở; tạo đủ bản ghi SPF/DKIM/DMARC; đặt PTR (rDNS) của IP về tên miền mail. |
| `Quota ổ đĩa chưa bật` | Chạy `/opt/xpanel/bin/enable-quota.sh` (ổ `/home` phải là ext4 hoặc xfs; xfs cần khởi động lại). |
| Cài đặt dừng giữa chừng | Xem `/var/log/xpanel-install.log`, khắc phục rồi **chạy lại lệnh** (an toàn khi chạy nhiều lần). |

Các lệnh kiểm tra nhanh trên VPS:

```bash
systemctl status xpanel-api xpanel-agent nginx --no-pager   # trạng thái dịch vụ
journalctl -u xpanel-api -u xpanel-agent -n 50 --no-pager    # log gần nhất
```

Báo lỗi hoặc góp ý: <https://github.com/langtupt/x-panel/issues> (đính kèm `/var/log/xpanel-install.log`).
