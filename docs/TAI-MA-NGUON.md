# Hướng dẫn tải mã nguồn lên website

Tài liệu này hướng dẫn đưa mã nguồn HTML, PHP, WordPress, Laravel, Node.js, Next.js và Go lên X-Panel
sao cho website chạy được ngay lần đầu.

## 1. Nguyên tắc chung

- Mã nguồn của website nằm ở `/home/<user>/www/<domain>/`. Trong *Quản lý file* thư mục này hiện là
  `www/<domain>`. Các file như `index.php`, `artisan`, `package.json` hay `go.mod` phải nằm **ngay trong**
  thư mục này, không nằm trong một thư mục con.
- Tạo website và chọn đúng **loại ứng dụng** trước khi tải mã lên. Runtime cần dùng (PHP, Composer,
  Node.js, Go) phải được admin cài sẵn ở trang **Applications**.
- Mọi lệnh build chạy bằng user Linux của chính tài khoản, trong giới hạn CPU/RAM của gói và tối đa
  30 phút. Website của tài khoản khác không đọc hay ghi được vào mã của bạn, và ngược lại.
- nginx không đọc `.htaccess`. Rewrite kiểu WordPress/Laravel (`index.php?...`) đã được cấu hình sẵn.
- File và thư mục bắt đầu bằng dấu chấm (`.env`, `.git`, ...) bị chặn truy cập từ web, trừ `.well-known`.

## 2. Ba cách tải mã lên

| Cách | Khi nào dùng |
|------|--------------|
| **Quản lý file** → *Tải lên* file `.zip`, sau đó *Giải nén* | Cách dễ nhất, tối đa 1 GB mỗi file |
| **SFTP** (cổng 22) | Mã lớn hoặc cần đồng bộ thường xuyên (FileZilla, WinSCP) |
| **FTP/FTPS** (cổng 21) | Dùng tài khoản FTP tạo ở trang FTP |

### 2.1 Nén mã nguồn đúng cách

Hãy nén **các file bên trong** thư mục dự án, không nén cả thư mục bên ngoài.

```
Đúng (mở file zip thấy ngay):          Sai (mở file zip thấy một thư mục):
  artisan                                 my-project/
  composer.json                             artisan
  app/  public/  routes/ ...                composer.json ...
```

Nếu file zip đã lỡ chứa một thư mục bọc ngoài: giải nén, mở thư mục đó, chọn tất cả và *Đổi tên/di chuyển*
lên `www/<domain>`, hoặc sửa **Thư mục gốc** của website cho khớp (chỉ với HTML/PHP).

Trên Linux/macOS:

```bash
cd my-project
zip -r ../site.zip . -x 'node_modules/*' 'vendor/*' '.next/*' '.git/*'
```

Trên Windows: mở thư mục dự án, chọn tất cả file → chuột phải → *Nén thành tệp ZIP*.

**Không cần tải lên** (panel tự tạo khi build, và tải lên sẽ chậm hoặc sai kiến trúc máy):
`node_modules/`, `vendor/`, `.next/`, `dist/`/`build/` của Node, file chạy Go đã build sẵn, `.git/`.

Định dạng giải nén được: `.zip`, `.tar`, `.tar.gz`/`.tgz`, `.tar.bz2`. Symlink và file đặc biệt trong file nén
bị bỏ qua.

### 2.2 Tải bằng Quản lý file

1. Mở **Quản lý file** → vào `www/<domain>`.
2. Xóa `index.html` mặc định do X-Panel tạo, nếu không trang mặc định có thể che mất mã của bạn.
3. *Tải lên* file `.zip` → chọn file → *Giải nén* vào chính thư mục hiện tại.
4. Xóa file `.zip` sau khi giải nén để không chiếm quota.

### 2.3 Tải bằng SFTP

- Máy chủ: IP hoặc tên miền của VPS, cổng **22**, giao thức SFTP.
- Đăng nhập: tên tài khoản hosting và **mật khẩu panel**.
- Sau khi đăng nhập bạn thấy `www/`, `tmp/`, `logs/`. Tải mã vào `www/<domain>/`. Không có shell SSH.

## 3. Hướng dẫn theo loại website

Sau khi tải mã lên, vào **Websites** → *Sửa* → bấm **Nhận diện từ mã nguồn** để panel gợi ý loại app, rồi lưu.
Với Laravel, Node.js, Next.js, Go: bấm **Build & restart** sau mỗi lần cập nhật mã và xem nhật ký ở trang Tasks.

Không cần vào VPS gõ lệnh. Sau khi cập nhật mã, bấm nút tương ứng ở dòng website:

| Thay đổi | Nút cần bấm |
|----------|-------------|
| Sửa file PHP (PHP thuần, WordPress) | Không cần; nếu vẫn thấy mã cũ, bấm **Restart** |
| Laravel: sửa mã PHP, view | **Restart** |
| Laravel: sửa `.env`, `config/`, `composer.json`, thêm migration | **Build & restart** |
| Node.js: sửa mã `.js` | **Restart** |
| Node.js: sửa `package.json`, mã TypeScript cần build | **Build & restart** |
| Next.js, Go: mọi thay đổi mã | **Build & restart** |
| Đổi biến môi trường trong form website | Lưu, rồi **Build & restart** |

### 3.1 HTML tĩnh

- Đặt `index.html` ngay trong `www/<domain>`. Không cần build.
- Site React/Vue/Vite đã build sẵn: tải nội dung thư mục `dist/` (hoặc `build/`, `out/`) lên, hoặc tải cả dự án
  và đặt **Thư mục gốc** là `dist`.

### 3.2 PHP thuần

- Loại **php**, chọn phiên bản PHP. File `index.php` ngay trong `www/<domain>` (hoặc trong **Thư mục gốc** đã chọn).
- Giới hạn mặc định: upload 128 MB, memory 256 MB, thời gian chạy 300 giây. Chỉnh riêng cho website bằng nút
  **PHP** ở dòng website trong trang Websites.
- PHP chỉ đọc được file trong home của tài khoản và `/tmp` (`open_basedir`).
- Có `composer.json`: thêm `composer install --no-dev` vào **Lệnh triển khai**, rồi *Build & restart*.

### 3.3 WordPress

1. Tạo database (MariaDB) và user database ở trang **Database**.
2. Tải mã WordPress (hoặc bản sao lưu của site cũ) lên `www/<domain>` theo mục 2.
3. Sửa `wp-config.php`: `DB_NAME`, `DB_USER`, `DB_PASSWORD`, `DB_HOST` = `localhost`.
4. Chuyển site cũ: import file `.sql` bằng phpMyAdmin, rồi đổi `siteurl`/`home` trong bảng `wp_options` nếu
   tên miền thay đổi.
5. Permalink dạng `/ten-bai-viet/` chạy sẵn, không cần `.htaccess`.
6. Bật HTTPS ở trang Websites (Let's Encrypt) khi tên miền đã trỏ về VPS.

### 3.4 Laravel

1. Loại **laravel**, chọn phiên bản PHP mà dự án yêu cầu (xem `composer.json` → `require.php`).
   **Thư mục gốc** tự đặt là `public`, không đổi.
2. Tạo database ở trang **Database**.
3. Tải mã lên **không kèm** `vendor/` và `node_modules/`.
4. Cấu hình môi trường. Có hai cách:
   - Tạo file `.env` trong `www/<domain>` (từ `.env.example`), gồm `APP_KEY`, `APP_URL`, `DB_*`.
     `DB_HOST=127.0.0.1`, `DB_PORT=3306` cho MariaDB, hoặc `DB_PORT=5433` cho PostgreSQL.
   - Hoặc nhập các biến `KEY=value` ở mục **Biến môi trường** của website.
   Chưa có `APP_KEY`: tạm thêm `php artisan key:generate --force` vào Lệnh triển khai, chạy Build một lần rồi xóa lệnh.
5. **Lệnh triển khai** gợi ý:
   ```
   php artisan migrate --force
   php artisan storage:link
   ```
6. Bấm **Build & restart**. Panel chạy `composer install --no-dev --optimize-autoloader`,
   `artisan config:cache` và `artisan view:cache`, rồi đến Lệnh triển khai.
7. Sửa `.env` sau này: bấm Build lại để xóa cache cấu hình cũ.
8. Dự án có frontend Vite: build tài nguyên ở máy bạn (`npm run build`) rồi tải `public/build/` lên cùng mã.

### 3.5 Node.js (Express, NestJS, Fastify...)

1. Loại **nodejs**, chọn phiên bản Node (để trống: bản mới nhất đã cài).
2. `package.json` **phải có** script `start`, ví dụ `"start": "node server.js"`. Nếu có script `build`
   (TypeScript, NestJS), panel sẽ chạy nó.
3. Ứng dụng **phải** lắng nghe cổng trong biến `PORT` tại `127.0.0.1`:
   ```js
   app.listen(process.env.PORT, process.env.HOST || "127.0.0.1");
   ```
   Không gắn cứng cổng 3000/8080. nginx chuyển tên miền về cổng này, kể cả WebSocket.
4. Nên tải kèm `package-lock.json` để `npm ci` cài đúng phiên bản.
5. Biến bí mật (chuỗi kết nối DB, khóa API): nhập ở **Biến môi trường**, không ghi vào mã. `PORT`, `HOST`,
   `PATH`, `HOME` do panel quản lý; `NODE_ENV=production` có sẵn.
6. Có Prisma/ORM: thêm `npx prisma migrate deploy` vào Lệnh triển khai.
7. Bấm **Build & restart**. App bị dừng bất thường sẽ tự khởi động lại sau 3 giây.

### 3.6 Next.js

1. Loại **nextjs**. `package.json` cần các script `"build": "next build"` và `"start": "next start"`.
2. Không tải `.next/` và `node_modules/` lên. Panel chạy `npm ci`/`npm install`, `npm run build`, rồi
   `next start` ở cổng được cấp.
3. Biến `NEXT_PUBLIC_*` được gắn vào mã lúc build: đổi chúng thì phải Build lại.
4. Site xuất tĩnh (`output: "export"`): có thể dùng loại **html** với Thư mục gốc `out` sau khi tự build ở máy.
5. Trong lúc build lại, phiên bản cũ vẫn phục vụ; chỉ gián đoạn vài giây khi khởi động lại.

### 3.7 Go

1. Loại **go**, chọn phiên bản Go.
2. `go.mod` và package `main` nằm ngay trong `www/<domain>`. Panel chạy `go build -o .xpanel-app .` rồi
   chạy file đó.
   Package main nằm ở `cmd/server`: tạo `main.go` ở thư mục gốc gọi vào package đó, hoặc chuyển package main
   ra thư mục gốc.
3. Lắng nghe đúng cổng:
   ```go
   http.ListenAndServe("127.0.0.1:"+os.Getenv("PORT"), handler)
   ```
4. Không tải file chạy đã build ở Windows/macOS lên; panel tự build cho đúng kiến trúc máy chủ.
5. Nên tải kèm `go.sum`. Dùng module riêng tư (repo private): chạy `go mod vendor` ở máy rồi tải kèm `vendor/`.

## 4. Lệnh triển khai

Ô **Lệnh triển khai** trong form sửa website chạy sau bước build mặc định ở mỗi lần *Build & restart*:

- Chạy bằng user của tài khoản, trong thư mục `www/<domain>`, có cùng biến môi trường với ứng dụng.
- `php` và `composer` tự dùng đúng phiên bản PHP của website.
- Lệnh nào lỗi thì Build dừng và ứng dụng **không** được khởi động lại; tiến trình cũ vẫn chạy cho đến khi
  bạn sửa lỗi và Build lại.
- Tối đa 8000 ký tự. Mỗi lệnh được in ra nhật ký Task để dễ dò lỗi.

## 5. Lỗi thường gặp

| Hiện tượng | Nguyên nhân và cách sửa |
|------------|-------------------------|
| Vẫn thấy trang mặc định của X-Panel | Còn `index.html` mặc định; xóa nó, hoặc mã nằm trong thư mục con (xem 2.1) |
| 403 Forbidden | Không có `index.php`/`index.html` trong Thư mục gốc; kiểm tra lại Thư mục gốc |
| 404 ở mọi trang Laravel | Thư mục gốc không phải `public` |
| 500 ở Laravel | `.env` thiếu `APP_KEY` hoặc sai `DB_*`; xem `storage/logs/laravel.log` |
| 502 Bad Gateway (Node/Next/Go) | App chưa build, app không nghe `PORT`, hoặc bị dừng do hết RAM của gói; xem *Logs* |
| Build lỗi "runtime chưa cài" | Nhờ admin cài Node.js/Go/PHP ở trang Applications |
| Build lỗi "Missing script: start" | Thêm script `start` vào `package.json` |
| Build hết thời gian | Build quá 30 phút hoặc thiếu RAM; tăng gói hoặc build tài nguyên ở máy rồi tải lên |
| Tải lên lỗi 413 | Form PHP tối đa 128 MB; file lớn hơn dùng Quản lý file (1 GB) hoặc SFTP |
| Hết dung lượng | Xóa file `.zip` đã giải nén, `node_modules` cũ, hoặc nâng gói |
