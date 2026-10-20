# X-Panel

Hosting control panel gọn nhẹ (Nginx + PHP-FPM) cho **Debian 12/13 và Ubuntu 22.04/24.04**: website (PHP, Laravel, Node.js, Next.js, Go), database MariaDB/PostgreSQL, FTP/SFTP, quản lý file, cron, chứng chỉ SSL (Let's Encrypt / tải lên / tự ký), email (Postfix + Dovecot + DKIM) và Roundcube Webmail.
Phân cấp tài khoản **Admin → Đại lý (Reseller) → Người dùng** với gói giới hạn tài nguyên.

## Kiến trúc

```
Trình duyệt ──HTTPS :8888──▶ nginx ──▶ xpanel-api (Go, user "xpanel", 127.0.0.1:8889)
                                          │  phục vụ UI Next.js (static export) + REST API
                                          │  PostgreSQL qua unix socket (peer auth)
                                          ▼
                              /run/xpanel/agent.sock (0660 root:xpanel)
                                          ▼
                              xpanel-agent (Go, root) — chỉ chạy action trong whitelist
```

| Thư mục | Nội dung |
|---|---|
| `apps/api` | Go API: auth (JWT 15 phút + refresh token xoay vòng), RBAC, users, audit log, migrations |
| `apps/agent` | Go agent chạy root: nhận diện distro (Debian/Ubuntu), user Linux, quota, vhost nginx, PHP-FPM, SSL |
| `apps/web` | Next.js 16 + Tailwind 4, build ra file tĩnh (`out/`) |
| `pkg/agentproto` | Giao thức API ↔ agent (danh sách action + client) |
| `deploy/` | systemd unit, nginx config, docker-compose cho dev |
| `installer/install.sh` | Script cài đặt 1 lệnh |
| `scripts/build-release.sh` | Đóng gói `dist/xpanel-linux-<arch>.tar.gz` |

## Cài đặt nhanh (1 lệnh)

Trên VPS **mới** chạy Debian 12/13 hoặc Ubuntu 22.04/24.04, đăng nhập bằng **root** (tối thiểu 1 GB RAM):

```bash
apt-get update && apt-get install -y curl ca-certificates && curl -fsSL https://raw.githubusercontent.com/langtupt/x-panel/main/installer/bootstrap.sh | bash
```

Đặt sẵn mật khẩu admin (không đặt thì sinh ngẫu nhiên). Cài xong, địa chỉ panel, tài khoản và mật khẩu hiển thị ngay trên màn hình; mật khẩu **không được lưu** trên máy chủ nên hãy ghi lại:

```bash
apt-get update && apt-get install -y curl ca-certificates && curl -fsSL https://raw.githubusercontent.com/langtupt/x-panel/main/installer/bootstrap.sh | ADMIN_PASSWORD='MatKhauManh123' bash
```

Đăng nhập bằng user thường có quyền sudo thì thay `| bash` bằng `| sudo bash` (và thêm `sudo` trước `apt-get`).
Bản cài tối giản của Debian/Ubuntu thường chưa có `curl` và `sudo`, nên lệnh trên cài `curl` trước.

Script tải bản phát hành mới nhất cho CPU của máy (amd64/arm64), kiểm tra SHA-256, cài đặt, bật quota ổ đĩa
và tối ưu VPS (swap khi RAM thấp, giới hạn log, MariaDB theo RAM, gzip, fail2ban). Chạy lại lệnh = nâng cấp.

Sau khi cài: mở **https://IP-VPS:8888**, vào **Applications** cài PHP, Node.js…, **Mail server** và
**Roundcube Webmail** nếu cần (webmail chạy ở **https://IP-VPS:2096/webmail/**).
Cổng cần mở ở firewall của nhà cung cấp VPS: 21, 22, 80, 443, 8888, 30000–30100 (FTP passive),
và khi dùng mail: 25, 465, 587, 110, 143, 993, 995, 2096.

## Cài từ mã nguồn

```bash
./scripts/build-release.sh amd64          # trên máy dev
scp dist/xpanel-linux-amd64.tar.gz root@server:
ssh root@server 'tar xzf xpanel-linux-amd64.tar.gz && cd xpanel-linux-amd64 && ADMIN_PASSWORD="..." ./install.sh'
```

Panel chạy ở **https://IP-server:8888** (nginx giữ cổng 8888 và chuyển vào API nội bộ 127.0.0.1:8889).

Installer sẽ cài nginx, PostgreSQL, MariaDB (chỉ nghe 127.0.0.1), quota; tạo user hệ thống `xpanel`, DB,
chứng chỉ self-signed, mở cổng 80/443/8888 nếu ufw đang bật; tạo tài khoản `admin` (mật khẩu lấy từ `ADMIN_PASSWORD`,
không có thì sinh ngẫu nhiên; chỉ hiển thị trên màn hình, không lưu thành file). Chạy lại script = nâng cấp, giữ nguyên
cấu hình và dữ liệu; chạy lại kèm `ADMIN_PASSWORD=...` để đặt lại mật khẩu admin.

Đổi mật khẩu một tài khoản bất kỳ trên server:

```bash
sudo -u xpanel env $(sudo cat /etc/xpanel/xpanel.env | xargs) /opt/xpanel/bin/xpanel-api reset-password -username admin -password '...'
```

### Bật quota ổ đĩa

Giới hạn dung lượng của gói chỉ có hiệu lực khi phân vùng chứa `/home` bật user quota:

```bash
sudo ./enable-quota.sh
```

- **ext4**: bật ngay (thêm `usrquota` vào fstab, remount, `quotaon`), không cần reboot.
- **xfs**: thêm `uquota` vào fstab (và `rootflags=uquota` vào kernel nếu `/home` nằm trên `/`), **cần reboot**.

Sau khi bật, mở từng gói và lưu lại để áp quota cho các tài khoản đã tạo trước đó.

## Phát triển (Windows/macOS/Linux)

```bash
docker compose -f deploy/docker-compose.dev.yml up -d   # PostgreSQL trên 127.0.0.1:5433

export XPANEL_DATABASE_URL="postgres://xpanel:xpanel@127.0.0.1:5433/xpanel?sslmode=disable"
export XPANEL_JWT_SECRET="dev-secret-dev-secret-dev-secret-0123456789"
export XPANEL_SECURE_COOKIES=false
export XPANEL_LISTEN=127.0.0.1:8888
export XPANEL_AGENT_DRY_RUN=true          # chỉ dùng khi dev trên máy không có agent (Windows/macOS)
go run ./apps/api/cmd/xpanel-api create-admin -username admin -password 'Admin@123!'
go run ./apps/api/cmd/xpanel-api serve                   # API + UI trên http://127.0.0.1:8888

cd apps/web && npm install && npm run dev                # UI trên http://localhost:3000 (proxy /api)
```

Agent chỉ chạy trên Linux. Gọi thử một action trên server: `xpanel-agent -call system.info`.

```bash
go test ./...                 # unit test (phân quyền, quy tắc gói, quota, TOTP, SQL/cron escaping…)
cd apps/web && npm run lint   # kiểm tra TypeScript

# End-to-end: cài bản release vào container systemd (Debian/Ubuntu) và chạy API thật
./scripts/build-release.sh amd64
./tests/e2e/run.sh debian     # hoặc ubuntu — website, PHP, SSL (Pebble), DB, FTP/SFTP, file, cron, 2FA
```

## Tài khoản và gói dịch vụ

- **Admin** tạo gói *đại lý* (tổng dung lượng cấp cho khách, số tài khoản tối đa, oversell) và gói *người dùng*.
- **Đại lý** tạo gói người dùng của riêng mình; mỗi giới hạn không được vượt gói đại lý. Khi tạo khách hoặc
  tăng gói, tổng dung lượng đã cấp không được vượt gói đại lý (trừ khi bật oversell); số tài khoản luôn bị giới hạn.
- Chỉ tài khoản **người dùng** là tài khoản hosting: có user Linux riêng (`/home/<tên>`, quyền 750, shell
  nologin) và quota ổ đĩa theo gói. Khóa tài khoản = hết hạn user Linux + đăng xuất mọi phiên.
  Xóa tài khoản = xóa user Linux và thư mục home.
- Agent chỉ đụng tới user Linux mang nhãn `xpanel` (GECOS), không bao giờ sửa/xóa user hệ thống.

## Website, PHP và SSL

- **PHP**: admin cài từng phiên bản 7.4–8.5 ở trang *Applications* (chạy nền). Debian dùng `packages.sury.org`,
  Ubuntu dùng `ppa:ondrej/php`. Các phiên bản chạy song song.
- **Website**: mã nguồn ở `/home/<user>/www/<domain>/<thư mục gốc>`. Mỗi site PHP/Laravel có một PHP-FPM pool và service
  riêng, chạy bằng chính user Linux đó, `open_basedir=/home/<user>/:/tmp/`.
  nginx được thêm vào group của user để đọc file tĩnh; các khách hàng khác không đọc được file của nhau.
- Mỗi thay đổi, agent dựng lại *toàn bộ* vhost + pool của user đó (`/etc/nginx/conf.d/xpanel/<user>/`),
  chạy `nginx -t` và chờ socket ứng dụng sẵn sàng trước khi trả thành công. Cấu hình nginx được khôi phục nếu kiểm tra cú pháp thất bại.
- Giới hạn từ gói: số website, phiên bản PHP được phép. Tên miền và alias là duy nhất trên toàn máy chủ.
- Khóa tài khoản → mọi website trả 503 "tạm ngưng" và pool PHP bị gỡ. Xóa website giữ lại mã nguồn.
- **SSL**: Let's Encrypt qua HTTP-01 (webroot `/var/lib/xpanel/acme`, thư viện lego), chạy dạng tác vụ nền.
  Tự gia hạn khi còn dưới 30 ngày (quét 12 giờ/lần). Đổi alias của site đang có SSL sẽ tự cấp lại.
  Tên miền phải trỏ về máy chủ và cổng 80 phải mở. Thử với CA khác (ví dụ Pebble) bằng
  `/etc/xpanel/agent.env`: `XPANEL_ACME_DIR=...`, `XPANEL_ACME_INSECURE=true`.

## Database (MariaDB)

- MariaDB chỉ nghe `127.0.0.1`; website kết nối bằng host `localhost` (socket) hoặc `127.0.0.1`.
- Database và user cùng tên, luôn có dạng `<tài khoản>_<tên>`; mỗi user chỉ có quyền trên database của mình
  (dấu `_` được escape trong `GRANT` để không thành ký tự đại diện). Agent kiểm tra lại tiền tố này.
- Tên/mật khẩu được kiểm tra theo bộ ký tự chặt trước khi đưa vào SQL; mật khẩu chỉ hiển thị một lần khi tạo.
- Giới hạn số database theo gói. Khóa tài khoản → `ACCOUNT LOCK` mọi user database; xóa tài khoản → xóa luôn database.

## SFTP, FTP, quản lý file, phpMyAdmin

- **SFTP** (cổng 22): mỗi tài khoản hosting đăng nhập bằng *mật khẩu panel*, bị chroot trong `/home/<user>`
  (home thuộc `root:<user>` 0750, user ghi trong `www/`, `tmp/`, `logs/`), chỉ SFTP, không có shell.
- **FTP** (cổng 21, FTPS explicit, passive 30000–30100): Pure-FTPd chỉ dùng virtual user của X-Panel
  (`<user>_<tên>`), mỗi tài khoản chroot vào một thư mục trong home, ghi file bằng uid của user (tính quota).
  Giới hạn theo gói (`max_ftp`). Khóa tài khoản → chặn đăng nhập FTP; xóa tài khoản → xóa luôn tài khoản FTP.
- **Quản lý file**: danh sách, tạo/sửa file (trình soạn thảo, Ctrl+S), đổi tên, phân quyền, xóa, nén .zip,
  giải nén .zip/.tar/.tar.gz, tải lên (tối đa 1 GB, có tiến độ), tải về. Mọi thao tác chạy trong tiến trình
  `xpanel-agent fshelper` đã `chroot` vào home và hạ quyền về uid/gid của user — symlink hay `../` không
  thoát ra ngoài được, quyền đúng như chính user đó.
- **phpMyAdmin**: admin bấm cài ở trang *Database* (tải bản mới nhất, kiểm tra SHA-256), chạy ở
  `https://IP:8888/phpmyadmin/` bằng pool PHP-FPM riêng (user `xpanel-pma`), đăng nhập bằng user database.

## Giới hạn CPU/RAM theo tài khoản

Mỗi site PHP/Laravel và ứng dụng Node.js/Next.js/Go có service riêng (`xpanel-app-<user>-<hash>.service`),
cấu hình trong `/etc/xpanel-site-apps/<user>/`, chạy trong cgroup `xpanel-<user>.slice`. CPU % và RAM của gói
được áp vào slice (`CPUQuota`, `MemoryMax`, không swap) qua `systemctl set-property` — có hiệu lực ngay khi đổi gói.
Request vượt RAM bị kernel kill (nginx trả 502) mà không ảnh hưởng website của khách khác. `pm.max_children`
tính theo RAM của gói (~64 MB/worker). Khóa tài khoản dừng các master này.
Lưu ý: cron và phiên SFTP chưa nằm trong slice này.

## Bảo mật tài khoản, cron, nhật ký

- **Xác thực 2 lớp (TOTP)** ở trang *Tài khoản của tôi*: quét QR (Google Authenticator, Authy…), secret
  được mã hóa AES-GCM trong DB (khóa dẫn xuất từ `XPANEL_JWT_SECRET` — đổi secret này thì 2FA phải bật lại).
  Tự đổi mật khẩu cũng ở đây (đăng xuất mọi phiên khác; với tài khoản hosting đổi luôn mật khẩu SFTP).
- **Đăng nhập thay**: admin/đại lý mở panel với quyền của tài khoản mình quản lý (có thanh báo và nút quay lại,
  ghi nhật ký; không đổi được mật khẩu/2FA trong lúc đăng nhập thay; không lồng nhau).
- **Cron**: lịch 5 trường hoặc `@hourly/@daily/...`, chạy bằng user Linux của tài khoản (ngoài chroot,
  như PHP); giới hạn theo gói (`max_cron`); tự dừng khi tài khoản bị khóa (cron kiểm tra tài khoản hết hạn).
- **Nhật ký**: admin xem tất cả, đại lý xem của mình và khách, người dùng xem của mình.

## API hiện có

| Method | Path | Quyền |
|---|---|---|
| POST | `/api/auth/login`, `/api/auth/refresh`, `/api/auth/logout` | công khai |
| GET | `/api/me`, `/api/me/overview` | đã đăng nhập |
| POST | `/api/me/password`, `/api/me/2fa/setup`, `/api/me/2fa/enable`, `/api/me/2fa/disable` | đã đăng nhập (không khi đang đăng nhập thay) |
| POST | `/api/auth/impersonate` / `/api/auth/unimpersonate` | admin, reseller (tài khoản mình quản lý) |
| GET/POST, PUT/DELETE | `/api/cron`, `/api/cron/{id}` | chủ tài khoản hoặc tài khoản quản lý |
| GET | `/api/audit?before=` | đã đăng nhập (phạm vi theo vai trò) |
| GET/POST | `/api/users` | admin (tất cả), reseller (khách của mình) |
| PATCH | `/api/users/{id}/status`, `/api/users/{id}/password` | admin, reseller (khách của mình) |
| PATCH | `/api/users/{id}/package` | tài khoản cha trực tiếp |
| DELETE | `/api/users/{id}` | admin, reseller (khách của mình) |
| GET/POST | `/api/packages` | admin, reseller (gói của mình) |
| PUT/DELETE | `/api/packages/{id}` | chủ sở hữu gói |
| GET/POST | `/api/sites` | admin (tất cả), reseller (site của khách), user (site của mình) |
| PUT/DELETE | `/api/sites/{id}` | chủ site hoặc tài khoản quản lý |
| POST/DELETE | `/api/sites/{id}/ssl` | chủ site hoặc tài khoản quản lý (POST tạo tác vụ) |
| GET/POST | `/api/databases` | admin (tất cả), reseller (của khách), user (của mình) |
| PATCH | `/api/databases/{id}/password` | chủ database hoặc tài khoản quản lý |
| DELETE | `/api/databases/{id}` | chủ database hoặc tài khoản quản lý |
| GET/POST | `/api/ftp` | admin (tất cả), reseller (của khách), user (của mình) |
| PATCH/DELETE | `/api/ftp/{id}/password`, `/api/ftp/{id}` | chủ tài khoản FTP hoặc tài khoản quản lý |
| GET | `/api/files?user_id=&path=` | chủ home hoặc tài khoản quản lý |
| POST | `/api/files/op` (`list read write mkdir rename delete chmod extract compress`) | như trên |
| POST/GET | `/api/files/upload`, `/api/files/download` (stream) | như trên |
| GET/POST | `/api/phpmyadmin` | trạng thái: đã đăng nhập; cài đặt: admin |
| GET | `/api/php` | đã đăng nhập |
| POST | `/api/php/{version}` | admin (tạo tác vụ cài đặt) |
| GET | `/api/tasks`, `/api/tasks/{id}` | admin (tất cả), người tạo tác vụ |
| GET | `/api/system/info` | admin |

Mọi request thay đổi dữ liệu phải gửi header `X-XPanel: 1` (chống CSRF).

## Lộ trình

- [x] Tuần 1–2: monorepo, API, agent, đăng nhập/RBAC, UI cơ bản, installer cho 3 distro
- [x] Tuần 3–4: gói dịch vụ (CRUD, giới hạn reseller), tạo Linux user + disk quota khi tạo tài khoản
- [x] Tuần 5–6: website (nginx vhost, PHP-FPM nhiều phiên bản sury/ondrej), SSL Let's Encrypt, hàng đợi task
- [x] Tuần 7–8: database MariaDB, FTP/SFTP, quản lý file, phpMyAdmin
- [x] Bổ sung MVP: 2FA TOTP, đăng nhập thay, tự đổi mật khẩu, cron, trang nhật ký, e2e trong `tests/e2e`
- [x] Tuần 9–10 (phần 1): giới hạn CPU/RAM theo tài khoản (cgroups, PHP-FPM riêng)
- [ ] Tiếp theo: backup/restore, thống kê tài nguyên theo tài khoản, rà soát bảo mật, bản v0.1

## Applications và ứng dụng website

Trang **Applications** (admin) thay trang PHP, liệt kê PHP, Composer, Node.js, Go,
MariaDB, PostgreSQL, phpMyAdmin và Adminer. Chọn phiên bản rồi cài; tác vụ chạy nền,
có trạng thái và nhật ký ở trang Tasks. Mỗi lần chỉ chạy một tác vụ cài đặt.
API cũ /api/php và /api/phpmyadmin vẫn tương thích.

- GET /api/apps; POST /api/apps/{id}/install với {"version":"24"} (admin).
- Tạo database với engine=mariadb (mặc định) hoặc engine=postgresql. Database cũ
  được migration sang mariadb. PostgreSQL khách hàng ở 127.0.0.1:5433 trong cluster
  customers, tách khỏi database panel; quản trị qua /adminer/ sau khi cài Adminer.
  MariaDB dùng localhost:3306 và /phpmyadmin/. Tên database vẫn duy nhất giữa hai engine.
- Website hỗ trợ html, php, laravel, nodejs, nextjs, go. Cài runtime cần dùng trong
  Applications trước. PHP/Laravel chọn php_version; Node.js/Go chọn runtime
  (để trống: mới nhất đã cài). Laravel dùng document root public.
- Mỗi site PHP/Laravel hoặc app chạy tiến trình có service systemd riêng trong
  xpanel-<user>.slice, chia sẻ giới hạn CPU/RAM của tài khoản. HTML do nginx phục vụ.
  Node.js/Next.js/Go được cấp cổng 10000–59999 và nginx reverse proxy về localhost.
  App Node.js tự viết phải đọc PORT/HOST và bind 127.0.0.1; Go cũng phải dùng PORT.
- Tải mã nguồn vào ~/www/<domain>, sửa site rồi bấm **Nhận diện từ mã nguồn** để
  gợi ý loại app. Có thể chọn **Tạo dự án mẫu** khi tạo site; thư mục phải trống.
  Next.js mẫu dùng package phiên bản latest ở lần tạo đầu, sau đó npm tạo lockfile.
- **Build & restart** chạy npm ci/install + build, go build, hoặc Composer + Laravel
  config/view cache bằng user Linux của khách trong slice của tài khoản (tối đa 30 phút).
  Không tự chạy database migrations. Node cần script start trong package.json.
- Biến môi trường riêng từng site nhập theo KEY=value; PORT, HOST, PATH, HOME do
  panel quản lý. PHP giữ môi trường trong pool riêng. Cấu hình được lưu ở DB panel
  và EnvironmentFile root-owned (0600); không đưa mật khẩu vào mã nguồn. Các biến hiển thị cho chủ site
  và tài khoản quản lý site. Logs đọc tối đa 200 dòng journal của service.
- GET /api/sites/{id}/detect, GET /api/sites/{id}/logs,
  POST /api/sites/{id}/build: chủ site hoặc tài khoản quản lý. Cấu hình app/env nằm trong
  POST/PUT /api/sites. Suspend dừng service và khóa user database của cả hai engine;
  activate phục hồi app đã build; xóa tài khoản gỡ service trước khi xóa Linux user.

Kiểm tra bổ sung: tests/e2e/applications.sh có assertions cho cài runtime qua API,
site mẫu, nginx, slice, database, suspend/activate và dọn service. Chạy trong container
systemd đã cài panel như tests/e2e/run.sh. Cài runtime cần truy cập các nguồn chính thức.

Kiểm thử trực tiếp trên VPS Ubuntu 24.04 ngày 19/10/2026: cài PHP 8.3, Node.js 24,
Go 1.27.2, PostgreSQL 18, Composer, Adminer, phpMyAdmin; sáu site mẫu hoạt động qua nginx;
MariaDB/PostgreSQL đổi mật khẩu; suspend/activate; dọn service; cô lập database giữa hai
khách; RBAC Applications; biến môi trường có ký tự đặc biệt. User quota đã xác minh
on sau khi cài module quota_v2 đúng kernel. Chưa hoàn tất bộ kiểm thử Debian trên VPS.

### Cập nhật danh sách phiên bản Applications

Danh sách lưu đệm 5 phút, có nút **Làm mới phiên bản**, nguồn và thời điểm kiểm tra.
PHP lấy các dòng stable từ php.net và đối chiếu gói thực tế trong Sury/Ondřej APT;
hiển thị cả bản vá cài được (hiện có PHP 8.5.11), không đưa RC/beta vào danh sách.
PHP 7.4/8.0/8.1 vẫn giữ cho website cũ, đã hết hỗ trợ upstream.
Composer, phpMyAdmin, Adminer hiển thị số phiên bản thực thay vì nhãn latest.
PHP/Node.js có nút cập nhật bản vá; cập nhật có thể khởi động lại runtime.
Lỗi nguồn được hiển thị rõ, dữ liệu cũ có cảnh báo và bị chặn cài cho đến khi xác minh lại.

Redis và Memcached có trong catalog, cài phiên bản từ kho APT hệ điều hành,
bao gồm bản vá bảo mật của distro; không cam kết trùng phiên bản upstream mới nhất.
Đây là dịch vụ cache dùng chung trên localhost, chưa có phân tách dữ liệu theo khách hàng.
MariaDB cài cùng panel; nâng cấp major MariaDB/PostgreSQL chưa có trong giao diện.

### Giao diện workspace và vận hành máy chủ

UI mặc định dùng preset shadcn `b1FQfCxG4` (Base Nova / neutral / Lucide),
sidebar responsive, Inter hỗ trợ tiếng Việt, các biểu mẫu mở bằng Dialog có focus trap.
Cài ứng dụng có bước xác nhận; thao tác ghi dữ liệu, build và cài đặt mở popup
trạng thái/nhật ký, có thể thu nhỏ và mở lại. Tải file hiển thị tiến độ upload thực.
Nhật ký cài đặt xuất hiện khi agent trả dữ liệu; không giả lập phần trăm cài đặt.

Dashboard admin tự lấy thông số máy chủ mỗi 5 giây: CPU, RAM, swap, ổ đĩa,
uptime, CPU model, kernel và tổng lưu lượng mạng từ lúc boot (không gồm loopback).
Biểu đồ CPU giữ tối đa 30 mẫu trong phiên xem hiện tại.

`POST /api/system/reboot` chỉ admin, yêu cầu JSON `{"confirmation":"REBOOT"}`,
chặn phiên impersonation và tác vụ pending/running; ghi audit rồi lên lịch
systemd reboot sau 10 giây. UI theo dõi boot ID để xác nhận máy chủ trở lại.
Kiểm thử scheduler dùng command giả; triển khai không tự reboot VPS.

Preview UI độc lập với VPS: chạy `python tests/ui/preview.py`, mở localhost:3010.
Toàn bộ dữ liệu và cài đặt trong preview đều mô phỏng; không dùng credential thật.
