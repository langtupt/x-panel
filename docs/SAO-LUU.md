# Sao lưu lên Google Drive và chuyển sang máy chủ mới

Mỗi **tài khoản hosting tự sao lưu lên Google Drive của chính mình**. Admin và đại lý không có tài khoản Google dùng
chung. Muốn cấu hình hộ khách, dùng *Đăng nhập thay*.

Mỗi bản backup gồm:
- thư mục home (website, mã nguồn);
- dump database (MariaDB và PostgreSQL), kèm mã băm mật khẩu của user database;
- hộp thư (maildir);
- cấu hình của tài khoản: website, database, hộp thư (kèm mã băm mật khẩu), chuyển tiếp, cron, FTP.

Dữ liệu được nén rồi mã hóa AES-256-GCM, khóa sinh từ **passphrase** của bạn, sau đó tải thẳng lên Drive theo
luồng, nên VPS không cần còn trống dung lượng. Google chỉ thấy dữ liệu đã mã hóa. **Mất passphrase thì không khôi
phục được.**

---

## 1. Tạo OAuth client trên Google Cloud (làm một lần, mất khoảng 5 phút)

1. Mở <https://console.cloud.google.com/> và đăng nhập tài khoản Google sẽ chứa backup.
2. Ở thanh trên cùng, chọn **Select a project → New project**, đặt tên ví dụ `xpanel-backup`, bấm **Create**, rồi
   chọn project vừa tạo.
3. **Bật Google Drive API:** mở <https://console.cloud.google.com/apis/library/drive.googleapis.com>, bấm
   **Enable**.
4. **Màn hình đồng ý (OAuth consent screen):** vào *APIs & Services → OAuth consent screen* (giao diện mới gọi là
   *Google Auth Platform → Branding*).
   - *App name*: `X-Panel backup`; *User support email* và *Developer contact*: email của bạn.
   - *Audience / User type*: **External**.
   - Mục **Test users**: bấm **Add users** và thêm chính email Google của bạn.
   - Không cần khai báo scope ở đây. Panel chỉ xin quyền `drive.file`, tức chỉ thấy các file do chính panel tạo,
     không đọc được file khác trong Drive của bạn.
5. **Tạo client:** vào *APIs & Services → Credentials → Create credentials → OAuth client ID*.
   - *Application type*: **TVs and Limited Input devices**. Bắt buộc đúng loại này, vì panel dùng luồng nhập mã
     trên `google.com/device`.
   - Bấm **Create**, rồi chép **Client ID** và **Client secret**.

> Ứng dụng ở trạng thái *Testing*: Google có thể yêu cầu đăng nhập lại sau 7 ngày. Để không bị gián đoạn, vào
> *OAuth consent screen → Publish app → Confirm*. Với scope `drive.file`, Google không bắt xác minh ứng dụng;
> màn hình đồng ý chỉ hiện cảnh báo "Google hasn't verified this app", bạn bấm *Continue*.

## 2. Kết nối trong X-Panel

1. Đăng nhập panel bằng tài khoản hosting và mở trang **Sao lưu**.
2. Dán **Client ID** và **Client secret** rồi bấm **Kết nối**. Panel hiện một mã dạng `ABCD-EFGH`.
3. Mở <https://www.google.com/device>, nhập mã, chọn tài khoản Google và bấm **Allow**. Vài giây sau trang Sao lưu
   báo *Đã kết nối* kèm email Google.
4. Đặt **passphrase** (ít nhất 12 ký tự) và cất ở nơi an toàn, ví dụ trình quản lý mật khẩu.
5. Chọn lịch (*hằng ngày* hoặc *hằng tuần*), giờ chạy và số bản giữ lại. Bấm **Backup ngay** để thử.

Backup nằm trong thư mục **X-Panel backups** trên Drive, tên file dạng `<tài-khoản>-YYYYMMDD-HHMMSS.xpb`.

## 3. Khôi phục trên cùng máy chủ

Trang Sao lưu → chọn bản backup → **Khôi phục** → chọn phần cần khôi phục (file, database, email) → gõ tên tài
khoản để xác nhận.

- File cùng tên bị ghi đè; các file mới hơn mà backup không có thì được giữ nguyên.
- Database được thay bằng dữ liệu trong backup.
- Nếu đã đổi passphrase sau khi tạo bản backup đó, nhập passphrase cũ vào ô *Passphrase khác*.

## 4. Chuyển tài khoản sang máy chủ mới

1. Cài X-Panel trên máy mới bằng lệnh cài đặt một dòng.
2. Admin tạo tài khoản hosting **cùng tên đăng nhập** với máy cũ, gán gói đủ lớn (số website, database, hộp thư,
   dung lượng).
3. Đăng nhập bằng tài khoản đó và vào **Sao lưu**. Kết nối Google Drive bằng **chính OAuth client cũ** (cùng Client
   ID và Client secret), nếu không Drive sẽ ẩn các file backup cũ. Đặt lại **đúng passphrase cũ**.
4. Bấm **Tìm backup trên Drive**. Các bản backup tạo từ máy cũ hiện trong danh sách với nhãn *máy chủ khác*.
5. Chọn bản mới nhất → **Khôi phục** → giữ đánh dấu **Tạo lại cấu hình còn thiếu** → xác nhận.

Panel sẽ:
- tạo lại website, database, tên miền email, hộp thư, chuyển tiếp, cron và tài khoản FTP mà máy mới chưa có, trong
  giới hạn của gói và với cùng các kiểm tra như khi tạo bằng tay;
- khôi phục file, dữ liệu database và email.

**Giữ nguyên mật khẩu:** user database và hộp thư dùng lại mật khẩu cũ, nên file cấu hình website (ví dụ
`wp-config.php`) và ứng dụng email chạy tiếp mà không phải sửa.

**Cần làm thêm sau khi khôi phục** (nhật ký tác vụ có liệt kê):
- Trỏ DNS tên miền về IP mới, rồi cấp lại SSL ở trang Website. Chứng chỉ không được chuyển theo.
- Cập nhật bản ghi **DKIM** mới cho từng tên miền email (xem ở trang Email), cùng MX/SPF nếu IP đổi.
- Đặt lại mật khẩu cho tài khoản FTP. Hộp thư tạo từ bản backup cũ (trước v1.2) cũng cần đặt mật khẩu mới.
- Tên miền đã thuộc tài khoản khác trên máy mới sẽ bị bỏ qua, không bao giờ bị chiếm.

## Câu hỏi thường gặp

**Admin có xem được Drive của khách không?** Không. Token Google và passphrase của mỗi tài khoản được mã hóa
riêng trong database. Admin và đại lý chỉ thấy danh sách backup và khôi phục được bằng passphrase đã lưu của tài
khoản đó.

**Kết nối báo `access_denied`?** Email Google chưa có trong *Test users* (bước 1.4), hoặc client không thuộc loại
*TVs and Limited Input devices*.

**Kết nối báo `invalid_client`?** Client ID hoặc Client secret bị chép sai hoặc thừa khoảng trắng.

**Backup có tốn dung lượng VPS không?** Không. Dữ liệu được tải lên Drive theo từng phần trong lúc nén.
