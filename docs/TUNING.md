# Tự tối ưu khi cài đặt và nâng cấp

Từ v1.1.4, bộ cài trực tiếp trên Debian/Ubuntu nhận diện RAM và số CPU khả dụng,
áp dụng profile cho máy hosting chạy chung nginx, PHP, MariaDB và PostgreSQL.
Không cần Docker hoặc chạy thêm một lệnh tối ưu riêng.

## Ngân sách mặc định

| RAM / CPU | nginx connections/worker | MariaDB buffer / connections | PostgreSQL shared / connections mỗi cluster | OPcache mỗi PHP master |
|---|---:|---:|---:|---:|
| 1 GB / 1 CPU | 2048 | 128 MB / 16 | 32 MB / 16 | 32 MB |
| 2 GB / 1 CPU | 4096 | 256 MB / 32 | 64 MB / 28 | 32 MB |
| 4 GB / 1 CPU | 4096 | 512 MB / 32 | 128 MB / 28 | 32 MB |
| 8 GB / 1 CPU | 8192 | 1024 MB / 32 | 256 MB / 28 | 64 MB |

RAM thực tế lấy từ `/proc/meminfo`, nên VPS bán dưới tên 4 GB có thể báo 3922 MB:
profile khi đó dùng MariaDB 490 MB và PostgreSQL 122 MB. Trên CPU nhiều hơn, số
kết nối tăng trong giới hạn RAM; trần MariaDB 200, PostgreSQL 96 mỗi cluster.
Các cache cố định có trần để không dành hết bộ nhớ máy lớn cho database.

Đây là ngân sách khởi đầu, không phải giới hạn RAM cứng của database. Các kết nối,
sort/hash/join, cache, extension và mã ứng dụng có thể dùng thêm RAM. Không tự tăng
mọi giới hạn lên thật cao. Hai cluster PostgreSQL dùng chung tài nguyên host, cùng
MariaDB và các ứng dụng; không áp dụng quy tắc dành 70–80% RAM riêng cho database.

## Các thiết lập được áp dụng

- nginx: nâng worker connections theo RAM, giữ giá trị đang cao hơn; descriptor
  limit 65536, kiểm tra `nginx -t`, lưu bản nginx.conf trước lần tối ưu đầu tiên.
- Kernel: `somaxconn` tối thiểu 4096, SYN backlog tối thiểu 8192, không hạ giá trị
  hiện tại cao hơn. Giữ swappiness 10, giới hạn journal và xoay log.
- MariaDB: buffer theo RAM, giới hạn kết nối theo CPU/RAM, bảng tạm 8 MB,
  slow-query log cho truy vấn từ 2 giây tại `/var/log/mysql/xpanel/slow.log`.
  Log được giới hạn quyền đọc, xoay hàng ngày/20 MB, giữ 7 bản; log có thể chứa SQL nhạy cảm.
- PostgreSQL: profile riêng cho `main:5432` và `customers:5433`, work_mem 2 MB,
  maintenance_work_mem 32 MB, không tạo parallel workers cho mỗi query, ghi truy vấn
  từ 2 giây qua hệ thống log của PostgreSQL. Cluster khách tạo sau cũng nhận profile.
- PHP: OPcache mặc định 32/64 MB, kiểm tra timestamp mã nguồn; cấu hình admin trong
  `99-xpanel.ini` có ưu tiên cao hơn `98-xpanel-performance.ini`. Áp dụng cho bản PHP
  hiện có và bản cài mới qua Applications.
- PHP của khách: `ondemand`, worker nghỉ 30 giây, tái tạo sau 500 request. Ước lượng
  64 MB/master và 96 MB/worker, chia ngân sách RAM tài khoản cho các site/version.
  Ví dụ 1 GB và hai site PHP có mặc định 4 worker/site, thay vì mỗi site nhận toàn
  bộ ngân sách. Mỗi pool tối thiểu 1 worker; nhiều pool hoặc app tốn RAM vẫn có thể
  chạm giới hạn cgroup. Cấu hình pool cập nhật khi đồng bộ site/gói.
- CPU/RAM của khách vẫn do systemd slice giới hạn; không cho ứng dụng khách dùng swap.
  Database dùng chung chưa có giới hạn CPU/RAM riêng cho từng khách. Không oversell
  tổng RAM/CPU nếu cần bảo đảm dịch vụ.
- API: file descriptor limit 65536 và pool PostgreSQL mặc định 4–16 kết nối tùy CPU;
  cấu hình `pool_max_conns` rõ ràng trong DSN vẫn được tôn trọng.

Trước khi restart, bộ cài kiểm tra cấu hình database/nginx. Nếu áp dụng thất bại,
nó phục hồi file cấu hình vừa thay đổi và thử khởi động lại dịch vụ. Nâng cấp có
restart database/nginx và reload PHP: thực hiện vào khoảng thời gian ít truy cập.
Không tự bật full-page cache hoặc Redis dùng chung vì có nguy cơ cache nhầm phiên
đăng nhập hoặc làm mất cách ly dữ liệu của khách.

Profile tính toán được lưu tại `/var/lib/xpanel/performance-profile.txt`. Chạy lại
lệnh cài để tính lại sau khi nâng RAM/CPU. Muốn bỏ qua bước áp dụng profile mới,
đặt `XPANEL_AUTO_TUNE=0` trước `bash`; tùy chọn này không hoàn tác cấu hình đã áp dụng
và không tắt những bước cơ bản sẵn có như swap/journal.

## Kiểm chứng

`tests/tuning-profile.sh` kiểm tra 28 tổ hợp RAM/CPU (768 MB–64 GB, 1–64 CPU),
giới hạn cache/kết nối và phần RAM còn lại theo mô hình ước lượng. Go tests kiểm
tra chia worker và đếm cả pool PHP cũ theo version lẫn pool mới theo site.

Kiểm thử trực tiếp trên Debian 13, 1 vCPU/3922 MB, ngày 20/10/2026, không Docker:

| Tình huống, 32 request đồng thời | Tổng request | Lỗi | p95 |
|---|---:|---:|---:|
| HTML trang đăng nhập qua nginx/API/TLS | 3000 | 0 | 37.38 ms |
| API đăng nhập sẵn, có truy vấn PostgreSQL | 3000 | 0 | 21.57 ms |
| PHP chạy 1000 lần SHA-256/request | 3000 | 0 | 37.73 ms |

Tổng 27000 request qua 9 tổ hợp tình huống/concurrency (1, 8, 32), không ghi nhận
lỗi HTTP/nội dung, swap không sử dụng. Site, user và package thử được dọn sau khi chạy.
Đây là kiểm thử tải ngắn từ chính VPS: client chia CPU với server, không đo mạng
Internet, không mô phỏng WordPress/Laravel/database nặng hay tải duy trì nhiều giờ.
Không dùng các số này làm cam kết lượng người dùng thực tế hoặc mức tăng so với bản cũ.

Chạy kiểm thử có giới hạn trên host được phép bằng `tests/load-http.py`. Với website
thực tế, đo p95/p99, HTTP 5xx, OOM, RSS, CPU, disk I/O, hàng đợi FPM và kết nối DB;
tăng tải từng bước trước khi điều chỉnh worker/cache/kết nối.
