# napkey-core

Control plane cho NapKey: người dùng, phiên đăng nhập, API key, và việc đẩy key sang data plane `kiro-go`.

NapKey core hiện bao phủ control-plane của Giai đoạn 2-5 trong `DESIGN.md`: người dùng và key tự phục vụ, usage ledger, bảng giá có cost basis, ví/PayOS, RBAC và vận hành.

## Phân vai

| Thành phần | Việc |
| --- | --- |
| `napkey-core` | Sở hữu Postgres. Nơi **duy nhất** tạo/thu key. Giữ user, session, audit log |
| `kiro-go` | Data plane. Xác thực traffic proxy, gọi Kiro API. Không biết gì về user |

Key chỉ hoạt động sau khi `napkey-core` đẩy được sang `kiro-go`. Đây là lý do việc tạo key thất bại thì trả lỗi ngay chứ không tạo nửa vời.

## Vì sao key không lưu thô

`api_keys` chỉ lưu SHA-256 của key. Bản thô hiện đúng một lần lúc tạo rồi biến mất.

Hệ quả cần biết trước khi sửa code ở đây: **không thể retry việc tạo key**. Không còn bản thô để gửi lại. Nên việc đẩy sang `kiro-go` làm **đồng bộ** ngay trong request tạo key, còn worker nền chỉ lo update và delete (hai việc này chỉ cần `remote_id`).

Nếu đẩy thất bại: xoá luôn dòng vừa tạo, trả 503, khách chưa thấy key nào. Cách khác là giữ bản thô chờ retry — tức là quay lại đúng vấn đề lưu key thô mà việc hash sinh ra để giải quyết.

## Driver Postgres tự viết

`internal/pgwire` là driver `database/sql` nói trực tiếp protocol v3 của PostgreSQL.

Lý do: môi trường dựng service này không truy cập được Go module proxy nên không vendor được `github.com/jackc/pgx`. Toàn bộ code phía trên viết theo `database/sql` và không import `pgwire` ngoài một dòng blank import trong `internal/store/store.go`, nên đổi sang `pgx` là đổi import chứ không phải viết lại.

Driver có: startup + SCRAM-SHA-256 (kèm channel binding), MD5, cleartext; extended query protocol; transaction đủ 4 mức isolation; TLS theo `sslmode`; huỷ query qua CancelRequest. Không có: COPY, LISTEN/NOTIFY, cursor.

## Chạy local

```bash
cp .env.example .env      # rồi sửa các giá trị CHANGE_ME
docker compose up -d postgres
go run .                  # tự chạy migration lúc khởi động
```

Với `MAIL_PROVIDER=log`, link xác minh email in ra log. Lấy từ đó để hoàn tất đăng ký khi chưa có SMTP.

Đặt `TRIAL_FINGERPRINT_SECRET` thành một chuỗi ngẫu nhiên tối thiểu 32 byte
và giữ nguyên qua các lần đổi `SESSION_SECRET`. Nếu bỏ trống, service tạm dùng
`SESSION_SECRET`; cách đó tương thích ngược nhưng việc đổi session secret sẽ làm
thay đổi fingerprint chống nhận trial nhiều lần.

Chỉ chạy migration rồi thoát:

```bash
go run . -migrate-only
```

## Test

```bash
go test ./...
```

Không cần Postgres thật. `internal/pgtest` dựng một server giả nói đúng protocol, còn `internal/pgwire` có server giả tự tính chữ ký SCRAM thật — protocol thì đúng byte hoặc là sai, mock trả byte cố định không kiểm được điều đó.

Giới hạn cần nói rõ: `pgtest` **không thực thi SQL**, test khai báo sẵn hàng trả về. Nên nó kiểm phía Go (bind tham số, scan, map lỗi) chứ không kiểm ngữ nghĩa SQL. Ràng buộc và transaction thật phải nghiệm thu trên Postgres thật trước khi mở cho khách.

## Endpoint

| Method | Path | Auth | Việc |
| --- | --- | --- | --- |
| `GET` | `/health` | không | Liveness |
| `GET` | `/ready` | không | Readiness: Postgres + data plane |
| `POST` | `/v1/auth/register` | không | Tạo tài khoản, gửi mail xác minh |
| `POST` | `/v1/auth/login` | không | Đăng nhập, set cookie |
| `POST` | `/v1/auth/logout` | cookie | Đăng xuất |
| `POST` | `/v1/auth/verify-email` | không | Xác minh bằng token |
| `POST` | `/v1/auth/resend-verification` | không | Gửi lại link |
| `POST` | `/v1/auth/forgot-password` | không | Gửi link đặt lại |
| `POST` | `/v1/auth/reset-password` | không | Đặt mật khẩu mới |
| `GET` | `/v1/auth/session` | session | Thông tin phiên |
| `POST` | `/v1/me/password` | session | Đổi mật khẩu |
| `GET` | `/v1/me/usage` | verified | Tổng usage + chi phí 30 ngày |
| `GET` | `/v1/me/usage/detail` | verified | Chuỗi theo ngày + phân tách theo model (`?from=&to=&keyId=`) |
| `GET` | `/v1/me/usage/records` | verified | Sổ usage từng request (`?keyId=&limit=&offset=`) |
| `GET` | `/v1/keys` | verified | Danh sách key |
| `POST` | `/v1/keys` | verified | Tạo key (trả bản thô một lần) |
| `GET` | `/v1/keys/{id}` | verified | Chi tiết key |
| `PATCH` | `/v1/keys/{id}` | verified | Đổi tên, bật/tắt |
| `DELETE` | `/v1/keys/{id}` | verified | Thu hồi |
| `GET` | `/v1/admin/users` | admin | Danh sách user |
| `POST` | `/v1/admin/users/{id}/status` | admin | Khoá/mở tài khoản |
| `POST` | `/v1/admin/users/{id}/quota` | admin | Cấp hạn mức tay |
| `GET` | `/v1/admin/audit` | admin | Audit log |
| `GET` | `/v1/admin/sync-drift` | admin | Key lạc trong data plane |
| `GET` | `/v1/admin/prices` | admin | Bảng giá theo thời gian |
| `POST` | `/v1/admin/prices` | admin | Mở kỳ giá mới (không sửa giá cũ) |
| `GET` | `/v1/admin/prices/quote` | admin | Thử tính tiền, không ghi gì |
| `GET` | `/v1/admin/usage-audit` | admin | Đối soát trước khi thu tiền |
| `POST` | `/v1/admin/keys/{id}/rebuild-counters` | admin | Dựng lại bộ đếm từ sổ |
| `POST` | `/internal/usage` | token nội bộ | `kiro-go` báo usage |

`verified` = đã đăng nhập **và** đã xác minh email. Địa chỉ chưa xác minh không tạo được key.

## Sổ usage và cách tính tiền

`usage_records` là sổ: một dòng cho một request, chỉ ghi thêm, không sửa. `api_key_usage` từ Giai đoạn 2 vẫn còn nhưng đổi vai — nay là **bộ đếm dẫn xuất** để `kiro-go` kiểm hạn mức và console đọc số tổng nhanh, không còn là nguồn sự thật.

Bốn quyết định đáng nói:

**Tiền là `bigint` micro-VNĐ, không phải float.** 1 VNĐ = 1.000.000 micros. Một token input Sonnet rơi vào khoảng 0,1 VNĐ, nên làm tròn về đồng ở tầng lưu trữ là mất trắng phần lẻ. `float64` thì sai số cộng dồn qua hàng triệu dòng, và một số dư không khớp với tổng các dòng của chính nó thì không bảo vệ được trước khách. Làm tròn **xuống** từng thành phần, nên số tiền khách phải trả không bao giờ bị làm tròn lên.

**Tách bốn loại token.** Input mới, output, cache read, cache write có giá lệch nhau hơn một bậc — cache read khoảng 1/10 input, cache write đắt hơn input. Gộp lại là tự bỏ tiền trên traffic nhiều cache. Bộ đếm Giai đoạn 2 chỉ có một con số tổng, nên nó không định giá được.

**Giá có kỳ hiệu lực, tính tiền theo giá lúc phục vụ.** `model_prices` không sửa dòng cũ; đổi giá là mở kỳ mới và đóng kỳ cũ. `cost_micros` đóng băng trên dòng usage lúc ghi. Nếu tính lại lúc đọc thì một lần đổi giá sẽ làm lệch mọi hoá đơn đã gửi. Một `EXCLUDE` constraint chặn hai kỳ giá chồng nhau cho cùng model — một bảng giá có thể diễn tả hai giá cho cùng một thời điểm là bảng không ai đối soát được.

**`request_id` là khoá chống trùng.** `kiro-go` retry khi báo usage thất bại; `ON CONFLICT (request_id) DO NOTHING` là toàn bộ cơ chế chặn tính tiền hai lần. Việc cộng bộ đếm phụ thuộc vào dòng insert có thật sự xảy ra, nên retry không đội usage lên.

**`occurredAt` chỉ được lệch tương lai tối đa 5 phút.** Giá được chọn theo lúc request xảy ra, nhưng một timestamp tương lai tùy ý có thể chọn sai kỳ giá. Báo cáo cũ vẫn được nhận để hỗ trợ retry; chỉ timestamp đi trước đồng hồ control plane quá xa mới bị từ chối.

Model không có giá vẫn được ghi, cost bằng 0, cắm cờ `unpriced`. Traffic đã phục vụ rồi — từ chối ghi là mất luôn dấu vết. Có dòng giá `'*'` tính theo giá Opus làm lưới đỡ: model lạ thì giả định đắt, vì mặc định rẻ nghĩa là bán lỗ cho tới khi có người phát hiện.

`GET /v1/admin/usage-audit` là cửa cần đọc sạch **trước** khi bật thu tiền: bộ đếm có khớp sổ, có gì phục vụ mà không có giá, và bao nhiêu phần trăm tiền đang dựa trên output token **ước lượng** chứ không phải đo được.

### Output token: đo được hay ước lượng

`kiro-go` ước lượng output token từ text đã render khi upstream không trả số thật (`proxy/token_estimator.go`). Trước Giai đoạn 3, code ghi đè số thật bằng số ước lượng ở cả bốn đường (Claude stream/non-stream, OpenAI stream/non-stream) — nay chỉ ước lượng khi upstream không nói gì, và cắm cờ `tokens_estimated` để phân biệt. Tính tiền một con số ước lượng như thể đo được thì không bảo vệ được, nên sự khác biệt này lưu theo từng dòng và hiện trong bản đối soát.

Đường OpenAI-compatible không có kế toán prompt cache, nên toàn bộ input tính giá input mới. Cách này **đắt hơn** cho khách so với đường Claude trên traffic nhiều cache; không đoán một tỉ lệ cache ở đây, vì một tỉ lệ đoán còn tệ hơn một tỉ lệ biết chắc là thận trọng.

## Bảo mật đã làm

- Mật khẩu: PBKDF2-HMAC-SHA256, 600.000 vòng, salt riêng mỗi mật khẩu, so sánh hằng thời gian. Argon2id mạnh hơn nhưng nằm ở `golang.org/x/crypto` — không có sẵn ở đây. Tham số lưu trong chuỗi hash nên nâng cost sau này không cần migration.
- Chống dò tài khoản: đăng nhập sai địa chỉ vẫn tốn CPU tương đương (`DummyVerify`), thông báo lỗi giống nhau. Không có thì thời gian phản hồi thành công cụ liệt kê tài khoản.
- Session lưu server-side, chỉ lưu hash. Đổi mật khẩu hoặc khoá tài khoản là thu hồi được ngay — JWT thì không.
- CSRF: double-submit cookie. `SameSite=Lax` là lớp của browser, token là lớp của server.
- CORS chỉ cho đúng origin console. Wildcard cộng cookie là cách để trang bên thứ ba đọc dữ liệu người đang đăng nhập.
- Rate limit trên mọi endpoint không cần đăng nhập, đếm trong Postgres chứ không trong memory (nhiều replica thì đếm trong process là vô nghĩa).
- Mọi query dùng tham số bind. Không có chỗ nào ghép SQL từ input.
- `X-Forwarded-For` chỉ tin khi ở sau proxy. Tin vô điều kiện thì ai cũng giả được khoá rate limit.

## Còn thiếu, thuộc giai đoạn sau

- **Đường đua hạn mức** (`DESIGN.md` mục 3.5) vẫn còn. `usage_records` đã là sổ thật, nhưng nó ghi **sau** khi request chạy xong. Chặn key lúc cạn ví cần reserve/settle nguyên tử ở mục 6 — Giai đoạn 4.
- **Ví, nạp tiền, PayOS**: checkout được ký phía server; webhook được xác minh HMAC và ghi có idempotent vào ledger append-only.
- **RPM/TPM**: được thực thi nguyên tử theo cửa sổ phút tại `kiro-go`.
- **Phân quyền admin**: role/permission nằm trong Postgres; `ADMIN_EMAILS` chỉ là bootstrap owner.
