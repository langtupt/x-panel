# Quét mã độc và tường lửa

## Quét mã độc (mọi tài khoản)

Trang **Quét mã độc** tìm webshell, mã độc chèn vào file hợp lệ, mã PHP giấu trong file ảnh, mã JavaScript chuyển
hướng và cấu hình `.htaccess`/`.user.ini` bị sửa trong thư mục home của tài khoản.

**Bộ máy quét**
- Bộ luật của X-Panel (luôn có): nhận diện `eval(base64_decode(...))`, chạy lệnh từ `$_POST`/`$_GET`, webshell phổ
  biến (WSO, b374k, FilesMan…), file PHP trong `wp-content/uploads`, chuỗi mã hóa dài bất thường…
- **ClamAV** (tùy chọn): admin bấm *Cài ClamAV* trên trang này để có thêm cơ sở dữ liệu chữ ký cập nhật hằng ngày.
  Dịch vụ `clamav-daemon` cần khoảng **1 GB RAM**.
  VPS ít RAM: admin bấm **Tắt clamd** (trang Quét mã độc) để giải phóng khoảng 1 GB; các lần quét sau dùng
  `clamscan`, chậm hơn và chỉ chiếm RAM trong lúc quét. Bấm **Bật clamd** để quét nhanh trở lại.

**Cách ly giữa các tài khoản:** trình quét chạy trong một tiến trình đã `chroot` vào thư mục home và chạy dưới user
Linux của chính tài khoản. Nó không đọc được file của tài khoản khác, kể cả khi đi theo symlink. ClamAV cũng chạy
dưới quyền user đó.

**Xử lý một phát hiện**

| Nút | Tác dụng |
|---|---|
| Phân tích AI | Gửi file tới ChatGPT bằng API key của chính tài khoản. Kết quả gồm kết luận (độc hại / đáng ngờ / an toàn), độ tin cậy và giải thích bằng tiếng Việt. |
| Làm sạch | Có khi AI xác định đây là file hợp lệ bị chèn mã. Panel thay file bằng bản đã gỡ mã độc và giữ bản gốc trong khu cách ly; bấm *Hoàn tác* để lấy lại bản gốc. Chỉ áp dụng khi file chưa thay đổi kể từ lúc phân tích. |
| Cách ly | Chuyển file vào `/var/lib/xpanel/quarantine/<tài khoản>/`: chỉ root đọc được và nằm ngoài web root, nên website không chạy được file đó nữa. |
| Khôi phục | Trả file đã cách ly về chỗ cũ và đánh dấu là báo nhầm. |
| Bỏ qua | Đánh dấu file an toàn; panel cảnh báo lại nếu nội dung file thay đổi. |
| Xóa hẳn | Xóa file vĩnh viễn, kể cả bản trong khu cách ly. |

**Tự động** (phần *Cài đặt*, chủ tài khoản tự đặt):
- **Lịch quét:**
  - *Mỗi giờ*: quét các file thay đổi trong giờ qua, cộng một lần quét toàn bộ mỗi ngày vào giờ đã chọn.
  - *Hằng ngày* hoặc *Hằng tuần*: quét toàn bộ.
- **Tự động cách ly:**
  - Có bật phân tích AI: chỉ cách ly khi AI kết luận *độc hại* với độ tin cậy từ 70% trở lên.
  - Không có AI: cách ly khi khớp luật mức *cao*.
  - File hợp lệ bị chèn mã không bị cách ly, để website không bị hỏng; bạn chọn *Làm sạch*.
- **Tự động phân tích AI:** tối đa 10 file mỗi lần quét, để kiểm soát chi phí API.
- **Thông báo:** chuông trên thanh tiêu đề của panel, cộng email nếu tài khoản có địa chỉ email và máy chủ có cài
  mail. Đại lý quản lý tài khoản đó cũng nhận thông báo trong panel.

**API key OpenAI:** tạo ở <https://platform.openai.com/api-keys> rồi dán vào trang Quét mã độc. Key được mã hóa
trong database và không bao giờ hiển thị lại. Mỗi lần phân tích, panel gửi tối đa 60 KB nội dung của file nghi nhiễm
tới OpenAI. Model mặc định là `gpt-4o-mini`, rẻ và đủ tốt cho việc này.

## Tường lửa (admin)

Trang **Tường lửa** được bật sẵn khi cài đặt với các giá trị dưới đây.

| Lớp | Mặc định | Ghi chú |
|---|---|---|
| Kết nối TCP mới mỗi IP | 100/giây | Bỏ gói SYN vượt mức (nftables) |
| Kết nối đồng thời tới 80/443 mỗi IP | 300 | Vượt mức bị từ chối |
| Request HTTP mỗi IP | 30/giây, burst 100 | nginx trả `429` |
| Request HTTP đồng thời mỗi IP | 100 | nginx trả `429` |
| IP liên tục vượt giới hạn request | chặn 30 phút | jail fail2ban `nginx-limit-req` |
| SSH/SFTP, FTP, SMTP, IMAP/POP3 | sai 6 lần / 10 phút → chặn 60 phút | fail2ban; IP tái phạm bị chặn lâu gấp đôi mỗi lần, tối đa 1 tuần |
| Đăng nhập panel theo IP | sai 10 lần / 15 phút → chặn IP 60 phút | |
| Đăng nhập panel theo tài khoản | sai 10 lần / 15 phút → khóa 15 phút | Chủ tài khoản nhận thông báo |

- **Luôn cho phép:** IP hoặc dải mạng trong danh sách này không bao giờ bị giới hạn hay chặn. Nên thêm IP văn phòng
  của bạn vào đây. Panel không cho lưu danh sách chặn có chứa IP bạn đang dùng.
- **Luôn chặn:** mọi gói tin từ IP hoặc dải mạng này bị bỏ.
- **IP đang bị chặn:** liệt kê mọi IP bị chặn (fail2ban, panel, chặn tay) kèm lý do và thời điểm hết hạn; có nút gỡ chặn.
- **Tài khoản bị khóa:** có nút mở khóa. Lưu ý: khóa theo tài khoản giúp chống dò mật khẩu từ nhiều IP, nhưng kẻ
  tấn công cũng có thể cố tình làm khóa tài khoản người khác. Hãy bật 2FA và giữ ngưỡng hợp lý.

Tường lửa nằm trong bảng nftables riêng `inet xpanel_fw` và chạy song song với ufw hoặc tường lửa sẵn có. Cấu hình
được lưu ở `/etc/xpanel/firewall.json` và được nạp lại khi khởi động máy.

> **Giới hạn:** tường lửa trên VPS chỉ giảm được flood nhỏ và tấn công tầng ứng dụng (HTTP flood, dò mật khẩu). Tấn
> công DDoS làm nghẽn băng thông mạng phải được lọc **trước khi tới VPS**: dùng dịch vụ chống DDoS của nhà cung cấp,
> hoặc đặt website sau Cloudflare.

**Bị chặn nhầm chính mình?** Đăng nhập SSH (hoặc console của nhà cung cấp) rồi chạy:

```bash
nft delete table inet xpanel_fw                           # tắt tạm cho đến lần khởi động lại agent
/opt/xpanel/bin/xpanel-agent fwban unban <IP-của-bạn>     # hoặc chỉ gỡ chặn một IP
```
