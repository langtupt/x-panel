# Quy trình đóng góp

Nhiều người (và trợ lý AI) cùng phát triển X-Panel. Để không ghi đè việc của nhau:

## Nhánh và Pull Request

- **Không đẩy thẳng lên `main`.** Mỗi việc làm trên một nhánh riêng: `<tên>/<việc>`, ví dụ `an/php-manager`,
  `claude/backup-migrate`.
- Mỗi người dùng **thư mục làm việc riêng** (một bản clone hoặc `git worktree add ../X-PANEL-<tên> -b <nhánh>`).
  Hai người không sửa chung một thư mục.
- Xong việc thì mở Pull Request vào `main`. Người còn lại xem qua, và CI (nếu có) phải đạt, rồi mới gộp.
- Trước khi mở PR, cập nhật nhánh theo `main` (`git pull --rebase origin main`) và tự chạy:

  ```bash
  gofmt -l apps pkg tests          # phải không in gì
  go vet ./... && go test ./...
  (cd apps/web && npx tsc --noEmit)
  ```

- Chuỗi giao diện mới viết bằng tiếng Việt trong `translate("…")` và thêm bản tiếng Anh vào
  `apps/web/lib/locales/en.json`.

## Phát hành

- Chỉ phát hành từ `main`, sau khi đã gộp PR. Mỗi lần phát hành do **một người** làm; báo trước để tránh trùng số
  phiên bản.
- Số phiên bản: sửa lỗi → tăng số cuối (`v1.1.6 → v1.1.7`); tính năng mới → tăng số giữa (`v1.2.0`).
- Thay đổi database luôn là migration mới (`apps/api/internal/db/migrations/00NN_*.sql`), không sửa migration cũ.
  Hai nhánh cùng thêm migration thì người gộp sau đổi số cho nối tiếp.

## Kiểm thử

- Bộ e2e: `./scripts/build-release.sh amd64 && TESTS="web db …" ./tests/e2e/run.sh debian|ubuntu|debian13`.
- Các bộ test tự xóa tài khoản và gói do chúng tạo (`tests/e2e/lib.sh`), nên chạy lại được trên VPS thật.
- **VPS test dùng chung:** trước khi cài lại, gỡ panel hoặc chạy test làm thay đổi hệ thống, báo trong nhóm và chờ
  người kia xác nhận. Không gỡ cài đặt khi người khác đang test.
- Không commit thông tin đăng nhập (`vps_*.txt`) và kết quả test (`tests/results`). Hai mục này đã có trong
  `.gitignore`.
