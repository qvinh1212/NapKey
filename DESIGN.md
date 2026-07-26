# NapKey — Thiết kế & Kế hoạch triển khai

> Tài liệu nguồn cho toàn bộ dự án. Design system trích từ `forge-ai.space` (tham chiếu thị giác), phần kiến trúc dựa trên đọc trực tiếp `backend/kiro-go`.

---

## 1. NapKey là gì

Cổng bán quyền truy cập API mô hình Claude. Khách nạp tiền, nhận API key, trỏ client (Claude Code, Cursor, SDK OpenAI/Anthropic) vào endpoint của NapKey, dùng bao nhiêu trừ bấy nhiêu.

Ba mặt của sản phẩm:

| Mặt | Người dùng | Nội dung |
| --- | --- | --- |
| Landing | Khách chưa đăng ký | Giá, model khả dụng, hướng tích hợp, đăng ký |
| Console | Khách đã đăng ký | Quản lý key, xem usage, nạp tiền, lịch sử giao dịch |
| Admin | Nội bộ | Pool tài khoản upstream, giá bán, đối soát, log |

Backend `kiro-go` hiện phục vụ mặt Admin và lớp proxy. Mặt Landing và Console chưa tồn tại.

---

## 2. Hiện trạng backend (đã đọc code, không phỏng đoán)

Go 1.21, một dependency duy nhất (`github.com/google/uuid`). Không database — toàn bộ state nằm trong `data/config.json`.

**Endpoint công khai**

| Path | Auth | Ghi chú |
| --- | --- | --- |
| `/v1/messages`, `/messages`, `/anthropic/v1/messages` | API key | Anthropic-compatible |
| `/v1/messages/count_tokens` | API key | |
| `/v1/chat/completions`, `/chat/completions` | API key | OpenAI-compatible |
| `/v1/responses`, `/responses` | API key | OpenAI Responses API |
| `/v1/models`, `/models` | không | Danh sách model + alias |
| `/v1/stats` | API key | Số liệu tổng |
| `/health`, `/` | không | |
| `/admin`, `/admin/api/*` | session cookie | Panel quản trị |

**Xác thực** (`proxy/auth.go`): key lấy từ `Authorization: Bearer ...` hoặc `X-Api-Key`. Công tắc tổng `RequireApiKey`; khi bật mà chưa cấu hình key nào thì chặn hết (fail-closed, đúng hướng). Khớp key → trả `ApiKeyEntry`, gắn ID vào request context để ghi usage.

**Hạn mức per-key** (`config.ApiKeyEntry`): `TokenLimit` (int64) và `CreditLimit` (float64), giá trị 0 = không giới hạn. Vượt → 429. Bộ đếm `TokensUsed` / `CreditsUsed` / `RequestsCount` cộng dồn, không tự reset.

**Pool tài khoản upstream** (`pool/`, `proxy/kiro*.go`): round-robin, tự refresh OAuth token, failover khi account lỗi. Nạp account qua AWS Builder ID, IAM Identity Center, Microsoft SSO, SSO token, credentials JSON, hoặc Kiro API key `ksk_...`.

**Admin API**: CRUD accounts + api-keys, `/status`, `/settings`, `/stats`, `/stats/reset`, `/logs`, `/thinking`, `/endpoint`, `/proxy`, `/prompt-filter`, `/version`, `/export`, `/api-keys/{id}/reset-usage`.

**Web admin**: `web/index.html` (36KB) + `app.js` (177KB) + `styles.css` (80KB), Tailwind chạy runtime qua `vendor/tailwindcss-browser/index.global.js` (274KB). i18n CN/EN.

**State hiện tại**: `data/config.json` có 1 key tên `napkey-local`, `accounts: []` rỗng. `.gitignore` đã loại `/data/` nên file chứa mật khẩu admin không bị commit — giữ nguyên quy tắc này.

---

## 3. Khoảng trống giữa hiện trạng và sản phẩm bán được

Xếp theo mức độ chặn đường:

**Chặn hoàn toàn**

1. **Không có khái niệm người dùng.** `ApiKeyEntry` không có `UserID`. Mọi key ngang hàng nhau, không ai sở hữu. Không thể làm self-service.
2. **Không có thanh toán.** Không cổng thanh toán, không sổ quỹ, không giao dịch.
3. **Không có sổ kế toán.** Chỉ có bộ đếm cộng dồn. Không biết ai dùng gì lúc nào, không đối soát được, không xuất hoá đơn.

**Rủi ro vận hành**

4. **`config.json` làm database.** `RecordApiKeyUsage` gọi `saveLocked()` — ghi lại toàn bộ file sau *mỗi* request. Vừa là điểm nghẽn throughput, vừa là nguy cơ mất/hỏng dữ liệu nếu tắt máy giữa lúc ghi. Đây là thứ phải thay trước khi mở cho khách thật.
5. **Kiểm hạn mức không nguyên tử.** Check limit xảy ra *trước* request, ghi usage xảy ra *sau*. Nhiều request song song cùng vượt qua vòng check rồi mới cộng dồn → khách có thể tiêu quá hạn mức. Cần đặt cọc (reserve) trước, quyết toán sau.
6. **Chỉ một mật khẩu admin.** Không phân quyền, không audit ai làm gì.
7. **Chưa có rate limit theo thời gian.** Chỉ có hạn mức tổng, không có RPM/TPM. Một khách có thể vắt cạn pool.

**Chi tiết cần dọn**

8. `GenerateApiKeyValue()` sinh key tiền tố `sk-`, nhưng key đang dùng lại là `nk-`. Chốt một tiền tố — đề xuất `nk_live_` / `nk_test_` — rồi sửa hàm sinh cho khớp.
9. Tailwind runtime 274KB trong admin: chấp nhận được cho nội bộ, không mang sang landing/console.

---

## 4. Kiến trúc đề xuất

Giữ `kiro-go` đúng vai trò nó đang làm tốt — data plane. Tách phần thương mại ra service riêng để không phải viết lại lớp proxy đã chạy ổn.

```
                    ┌──────────────────────────────┐
   Khách ─────────► │  Landing + Console (Next.js) │
                    └──────────────┬───────────────┘
                                   │ REST, session cookie
                    ┌──────────────▼───────────────┐
                    │  napkey-core (Go)            │
                    │  user, key, ví, đơn hàng,    │
                    │  sổ usage, webhook thanh toán│
                    └──────┬───────────────┬───────┘
                           │ Postgres      │ cấp/thu key
                    ┌──────▼──────┐ ┌──────▼───────────────┐
   SDK/CLI ────────►│  Postgres   │ │  kiro-go (data plane)│
   của khách        └─────────────┘ │  proxy, pool, dịch   │
        │                           └──────────┬───────────┘
        └──────────────────────────────────────┘
                  gọi trực tiếp /v1/*      │
                                    ┌──────▼──────┐
                                    │  Kiro API   │
                                    └─────────────┘
```

**Vì sao tách hai service**: lớp proxy phải chịu tải SSE dài phút, còn lớp thương mại thì transaction nặng và cần Postgres. Ép chung một binary sẽ khiến mỗi lần deploy tính năng thanh toán là một lần ngắt stream của khách đang chạy.

**Hợp đồng giữa hai service**

- `napkey-core` là nơi duy nhất tạo/xoá key, đẩy sang `kiro-go` qua `/admin/api/api-keys`.
- `kiro-go` báo usage về `napkey-core` sau mỗi request (webhook nội bộ hoặc hàng đợi), `napkey-core` ghi sổ và trừ ví.
- Khi ví cạn, `napkey-core` gọi PUT tắt `Enabled` của key.

Trước khi có `napkey-core`, `kiro-go` vẫn chạy độc lập được — dùng để bán key thủ công.

### Hạ tầng triển khai

Coolify tự quản tại `https://coolify.qbo.io.vn`. DNS của domain này đang đi qua Cloudflare (bản ghi A trả về `104.21.90.239` / `172.67.162.204` — dải Cloudflare), nên phần webhook bên dưới có một cái bẫy cần xử lý trước.

| Thành phần | Dạng | Ghi chú |
| --- | --- | --- |
| `postgres` | Coolify database service | **Không** mở port ra internet, chỉ nói chuyện qua network nội bộ của Docker |
| `napkey-core` | Docker app | Nhận webhook Casso, giữ sổ, cấp key |
| `kiro-go` | Docker app | Cần volume bền cho `data/config.json` |
| `napkey-web` | Docker app | Next.js standalone |

Việc ở tầng hạ tầng không bỏ qua được:

- **Backup Postgres**: bật scheduled backup của Coolify đẩy sang S3, rồi **thử restore một lần** vào một database rác. Backup chưa restore thử thì chưa tính là backup.
- **Webhook phải public HTTPS**: Casso từ chối URL nội bộ/localhost và timeout request sau 5 giây. Traefik của Coolify + Let's Encrypt lo phần cert; nếu bật Cloudflare proxy thì đặt SSL mode **Full (strict)**.
- **Cloudflare có thể chặn Casso**: tài liệu Casso yêu cầu whitelist IP của họ khi site dùng Cloudflare hoặc dịch vụ chống DDoS, nhưng không công bố dải IP — phải hỏi Casso rồi tạo WAF skip rule riêng cho path webhook. Bỏ qua bước này thì webhook có thể bị chặn không đoán trước, và tiền khách đã chuyển sẽ không vào ví.
- **Một VPS là một điểm chết**. Chấp nhận được ở giai đoạn đầu, nhưng ví và sổ usage nằm trong Postgres đó — backup là thứ duy nhất chắn giữa một sự cố ổ đĩa và việc mất sổ tiền của khách.

---

## 5. Data model (Postgres)

Chỉ nêu bảng và cột mang tính quyết định; kiểu dữ liệu chốt luôn để tránh tranh luận sau.

**`users`** — `id uuid pk`, `email citext unique`, `password_hash text`, `email_verified_at timestamptz`, `status text` (`active` / `suspended`), `created_at`.

**`api_keys`** — `id uuid pk`, `user_id uuid fk`, `name text`, `key_prefix text` (8 ký tự đầu, để hiển thị), `key_hash text` (SHA-256 của key đầy đủ), `last_four text`, `enabled bool`, `rpm_limit int`, `tpm_limit int`, `revoked_at timestamptz`, `last_used_at timestamptz`, `created_at`.

> Lưu **hash**, không lưu key thô. Key thô chỉ hiện đúng một lần lúc tạo. Đây là điểm khác biệt so với `kiro-go` hiện tại (đang lưu thô trong `config.json`) và là lý do `napkey-core` phải nắm quyền tạo key.

**`wallets`** — `user_id uuid pk`, `balance_micros bigint`, `currency text not null default 'VND'`, `updated_at`.

> Đơn vị nội bộ là **micro-VNĐ**: `1 VNĐ = 1_000_000 micros`, lưu `bigint`, **không dùng float**. Chọn micro vì giá một token nhỏ hơn một đồng — mức $3/1M token input rơi vào khoảng 78 VNĐ cho 1k token, tức 0,078 VNĐ một token. Làm tròn về đồng ở tầng lưu trữ là mất trắng phần lẻ đó.
>
> `CreditsUsed` bên `kiro-go` đang là `float64` — sai số cộng dồn qua hàng triệu request sẽ lệch sổ. Quy đổi sang micro ngay tại biên khi nhận usage.
>
> Hiển thị: làm tròn **xuống** về VNĐ nguyên (`floor`) cho số dư, và không bao giờ làm tròn lên số tiền khách phải trả. Giữ micro trong DB, chỉ đồng ở giao diện.

**`ledger_entries`** — `id bigserial pk`, `user_id uuid fk`, `wallet_id uuid fk`, `kind text` (`topup` / `usage` / `refund` / `adjustment` / `hold` / `hold_release`), `amount_micros bigint` (dương = vào, âm = ra), `balance_after_micros bigint`, `ref_type text`, `ref_id text`, `idempotency_key text unique`, `created_at`.

> Append-only — sửa hay xoá dòng đã ghi là không được, muốn đảo thì ghi dòng `adjustment` đối ứng. Số dư trong `wallets` là cache của tổng sổ; có job đối chiếu định kỳ.
>
> `idempotency_key` chặn nạp trùng: với nạp ví qua Casso, đặt `casso:<transaction_id>`. Đây là lớp bảo vệ thứ hai sau ràng buộc `unique` ở `payment_events` — Casso retry tới 17 lần cho một sự kiện nên hai lớp là hợp lý, không phải dư thừa.

**`usage_records`** — `id bigserial pk`, `user_id uuid`, `api_key_id uuid`, `request_id text unique`, `model text`, `input_tokens int`, `output_tokens int`, `cache_read_tokens int`, `cache_write_tokens int`, `cost_micros bigint`, `upstream_account_id text`, `latency_ms int`, `status text`, `created_at timestamptz`.

> Phân biệt cache read/write vì giá khác nhau rõ rệt — gộp lại là tự bỏ tiền. Partition theo tháng khi vượt ~10M dòng.

**`topup_orders`** — `id uuid pk`, `user_id uuid fk`, `memo_code text unique`, `expected_amount_micros bigint`, `received_amount_micros bigint not null default 0`, `provider text not null default 'casso'`, `bank_account_number text`, `status text` (`pending` / `paid` / `underpaid` / `expired` / `cancelled`), `expires_at timestamptz`, `paid_at`, `created_at`.

> Đổi tên từ `orders` cho đúng việc nó làm: đây là lệnh nạp ví, không phải đơn hàng sản phẩm. `memo_code` là chuỗi khách phải ghi vào nội dung chuyển khoản — chi tiết ở mục 6.1.

**`payment_events`** — `id bigserial pk`, `provider text`, `provider_tx_id text`, `signature_verified bool`, `payload jsonb`, `matched_order_id uuid null`, `status text` (`credited` / `duplicate` / `unmatched` / `rejected`), `received_at timestamptz`, `processed_at timestamptz`, `unique (provider, provider_tx_id)`.

> Mọi webhook đi vào bảng này **trước** khi chạm tới ví, kể cả webhook sai chữ ký (ghi `rejected` và giữ payload để điều tra). Ràng buộc `unique (provider, provider_tx_id)` là chốt chống trùng: Casso gọi lại một sự kiện tối đa 17 lần, và dashboard của họ còn có nút replay thủ công. Thiếu ràng buộc này thì một giao dịch nạp 500k có thể vào ví nhiều lần.

**`model_prices`** — `id`, `model text`, `input_micros_per_1k bigint`, `output_micros_per_1k bigint`, `cache_read_micros_per_1k bigint`, `cache_write_micros_per_1k bigint`, `effective_from timestamptz`, `effective_to timestamptz`.

> Giá tính bằng **micro-VNĐ trên 1.000 token**. Giá có hiệu lực theo thời gian: tính tiền tra bảng theo `created_at` của usage, không lấy giá hiện tại. Đổi giá không được làm sai lệch usage đã ghi trong quá khứ.
>
> Giá gốc của Anthropic niêm yết bằng USD, còn mình bán bằng VNĐ — chênh lệch tỷ giá là rủi ro của mình. Chốt tỷ giá quy đổi thủ công trong bảng này (một hằng số vận hành, không gọi API tỷ giá lúc tính tiền), cộng biên an toàn cho biến động, và rà lại khi tỷ giá lệch quá ngưỡng đã định.

**`audit_logs`** — `id`, `actor_type` (`user` / `admin` / `system`), `actor_id`, `action`, `target_type`, `target_id`, `metadata jsonb`, `ip inet`, `created_at`.

---

## 6. Luồng tính tiền (giải bài toán ở mục 3.5)

Đặt cọc trước, quyết toán sau:

1. Request tới `kiro-go`, key hợp lệ.
2. `napkey-core` tạo **hold**: trừ tạm số tiền ước tính (theo input tokens + trần output của request) khỏi số dư khả dụng. Ghi `ledger_entries` kind=`hold`.
3. Stream chạy. Kết thúc, có số token thật.
4. Quyết toán: ghi `usage_records`, ghi `ledger_entries` kind=`usage` với số thật, giải phóng hold. Chênh lệch trả lại ví.
5. Nếu request lỗi giữa đường: giải phóng toàn bộ hold, không tính tiền phần chưa nhận được.

Kiểm tra số dư khả dụng = `balance - tổng hold đang mở`. Một câu `UPDATE ... WHERE balance_micros - held >= :cost` để đảm bảo nguyên tử; không đọc-rồi-ghi.

Hold quá hạn (client ngắt kết nối, process chết) được job dọn sau 15 phút.

---

## 6.1 Nạp ví qua Casso

Casso không phải cổng thanh toán theo nghĩa giữ tiền hộ. Nó đọc biến động số dư tài khoản ngân hàng của mình rồi bắn webhook. Tiền đi trực tiếp từ khách vào tài khoản ngân hàng NapKey, Casso chỉ đóng vai người thông báo. Hệ quả thiết kế: **không có bước redirect sang trang cổng, không có `returnUrl`, và không thể hủy giao dịch từ phía mình.** Đối soát là toàn bộ công việc.

Dùng **Webhook V2** (payload một giao dịch, chữ ký HMAC-SHA512), không dùng webhook V1 (payload mảng, chỉ so sánh secret trong header).

### Luồng

1. Khách chọn số tiền nạp ở `/console/billing`. Tối thiểu 20.000 VNĐ (dưới mức này phí xử lý và rủi ro đối soát không đáng).
2. `napkey-core` tạo `topup_orders`: sinh `memo_code`, `expected_amount_micros`, `expires_at = now() + 60 phút`.
3. Giao diện hiện **mã VietQR** (nhúng sẵn số tiền và nội dung chuyển khoản) kèm số tài khoản, tên chủ tài khoản, số tiền, và `memo_code` — mỗi trường có nút copy riêng. QR quan trọng hơn phần chữ: khách quét thì nội dung chuyển khoản không thể sai, còn gõ tay thì sai thường xuyên.
4. Khách chuyển khoản. Trang billing poll trạng thái đơn mỗi 3 giây (đơn giản hơn SSE, và cửa sổ chờ chỉ vài phút).
5. Casso phát hiện giao dịch mới → `POST` vào `https://api.napkey.../webhooks/casso`.
6. `napkey-core` xử lý theo mục dưới, ghi ví, đổi trạng thái đơn sang `paid`.
7. Poll thấy `paid` → giao diện hiện số dư mới.

### `memo_code`

Đây là điểm nối duy nhất giữa một lệnh chuyển khoản và một tài khoản người dùng. Sai ở đây là ghi tiền vào ví người khác.

- Định dạng: `NK` + 6 ký tự từ bộ **Crockford base32** (bỏ `I`, `L`, `O`, `U`) → ví dụ `NK7F3QK2`. Ngắn để khách gõ tay được, đủ không gian (32^6 ≈ 1,07 tỷ) để không cần lo trùng.
- So khớp bằng cách **chuẩn hoá** `description` rồi tìm chuỗi: bỏ dấu, viết hoa, loại mọi ký tự không phải chữ-số. Bắt buộc, vì ngân hàng thường chèn tiền tố kiểu `CHUYEN TIEN TU ... ND:` và cắt bớt độ dài nội dung.
- Chỉ hết hạn `pending`, **không** hết hạn khả năng ghi nhận: nếu khách chuyển muộn sau khi đơn đã `expired`, tiền vẫn phải vào ví họ. Quét `memo_code` trong toàn bộ đơn của user chứ không chỉ đơn còn hiệu lực.

### Xác thực webhook

Header `X-Casso-Signature` có dạng `t=<unix_ms>,v1=<hex>`. Quy trình theo tài liệu Casso:

1. Tách `t` và `v1`.
2. Sắp xếp key của **toàn bộ** object JSON (bao gồm cả `error` và `data`) tăng dần A→Z, đệ quy.
3. Serialize thành JSON **không khoảng trắng** (Python phải truyền `separators=(',', ':')`; `JSON.stringify` của JS mặc định đã đúng).
4. Chuỗi cần ký: `t + "." + json`.
5. `HMAC-SHA512(key = Key bảo mật, data = chuỗi trên)`, mã hoá hex.
6. So với `v1` bằng **so sánh hằng thời gian** (`hmac.Equal` trong Go), không dùng `==` chuỗi thường.

> **Cảnh báo về sample code của Casso**: tôi đã chạy thử vector mẫu trong `CassoHQ/casso-webhook-v2-verify-signature` (`javascript.js`) với đúng thuật toán trong README của chính repo đó — chữ ký không khớp. Cả bản JS và bản Python trong repo đều ký toàn bộ envelope, nên đây không phải chuyện nhập nhằng `data` vs envelope; vector mẫu trong repo đơn giản là không tự nhất quán. **Đừng dùng nó làm test fixture.** Cách lấy vector đúng: bấm **Gọi thử** trong giao diện Casso, log lại nguyên văn body + header, rồi cố định cái đó làm fixture. Trước khi đi live, xác nhận bằng một lần chuyển khoản thật số tiền nhỏ.

Thêm hai lớp phòng vệ:

- **Chống replay theo thời gian**: từ chối nếu `|now - t| > 5 phút`. Chữ ký hợp lệ vẫn có thể bị phát lại.
- **So sánh trên raw body**: verify chữ ký trước khi parse, và ký lại đúng byte đã nhận. Không decode-rồi-encode-lại rồi mới ký.

### Xử lý sự kiện

Casso yêu cầu phản hồi **200 trong vòng 5 giây**, nếu không sẽ gọi lại tới 17 lần trong 24 giờ (giãn theo Fibonacci), rồi chuyển webhook sang `PAUSED` sau 24 giờ toàn lỗi và `DISABLE` sau 7 ngày. `DISABLE` là trạng thái mất dữ liệu: giao dịch phát sinh khi đó **không** replay lại được. Nghĩa là handler phải nhanh và gần như không bao giờ lỗi.

Cách đạt điều đó: handler chỉ làm ba việc — verify chữ ký, `INSERT` vào `payment_events`, trả `200`. Việc ghi ví làm ở worker riêng đọc từ bảng đó. Ghi một dòng thì mất vài ms và gần như không thể timeout, còn nhét cả logic đối soát vào handler thì mỗi lần Postgres chậm là một bước tiến tới `DISABLE`.

Bật **Strict mode** ở Casso và trả `{"success": 1}` kèm `200` — để một lỗi 500 trả về HTML vẫn bị hiểu là thất bại và được retry, thay vì bị coi là thành công.

Bảng quyết định của worker:

| Tình huống | Xử lý |
| --- | --- |
| `amount <= 0` (tiền ra) | Bỏ qua, ghi `rejected`. Chỉ tiền vào mới nạp ví |
| `provider_tx_id` đã tồn tại | `duplicate`, không chạm ví |
| Không tìm thấy `memo_code` | `unmatched`, chờ admin gán tay ở trang đối soát |
| Khớp đơn, đúng số tiền | Nạp ví, đơn → `paid` |
| Khớp đơn, thiếu tiền | Nạp **đúng số thực nhận**, đơn → `underpaid`, hiện phần còn thiếu cho khách |
| Khớp đơn, thừa tiền | Nạp **đúng số thực nhận**, đơn → `paid`, phần thừa nằm lại trong ví |

Nguyên tắc xuyên suốt: **luôn ghi số tiền thật nhận được**, không ghi số tiền đơn hàng mong đợi. Khách gõ sai số tiền là chuyện thường; ghi theo kỳ vọng thì hoặc mình mất tiền, hoặc khách mất tiền.

Định danh giao dịch: dùng `data.id` của Casso làm `provider_tx_id` (tài liệu ghi đây là mã định danh duy nhất do Casso quy định). Lưu thêm `data.reference` (mã của ngân hàng) để tra soát với sao kê.

### Hậu kiểm

Webhook thất bại là chuyện sẽ xảy ra. Job chạy mỗi 15 phút gọi `GET https://oauth.casso.vn/v2/transactions` (header `Authorization: Apikey <key>`) lấy giao dịch trong 24 giờ gần nhất, đối chiếu với `payment_events` theo `id`, ghi bù phần thiếu qua đúng đường worker ở trên. Đây là lưới an toàn cho cả trường hợp Cloudflare chặn webhook mà mình chưa biết.

Cộng thêm một cảnh báo vận hành: có giao dịch `unmatched` quá 30 phút thì báo admin. Đó là tiền của khách đang treo.

### Rút tiền

Không hỗ trợ. Số dư đã nạp chỉ dùng để gọi API, không hoàn về tài khoản ngân hàng. Phải nói rõ điều này ở trang nạp tiền và trong điều khoản **trước** khi khách chuyển khoản, không phải sau.

---

## 7. Design system

Trích bằng cách đọc computed style trực tiếp trên `forge-ai.space`. Đây là số thật, không phải ước lượng.

### Màu

```css
--bg:            #000000;   /* nền đen tuyệt đối */
--surface:       rgba(255,255,255,0.02);
--surface-hover: rgba(255,255,255,0.05);
--border:        rgba(255,255,255,0.10);

--text:          #ffffff;
--text-muted:    #a3a3a3;   /* neutral-400 */
--text-dim:      #525252;   /* neutral-600 */
--text-subtle:   #a1a1aa;   /* zinc-400, dùng cho nav */

--accent:        #10b981;   /* emerald-500 — trạng thái tốt, CTA phụ */
--accent-soft:   rgba(16,185,129,0.10);
--accent-light:  #34d399;   /* emerald-400 */

--danger:        #ef233c;   /* đỏ tươi — filter đang chọn, cảnh báo */
--danger-soft:   rgba(239,35,60,0.10);

--info:          #60a5fa;   /* blue-400 */
--warn:          #facc15;   /* yellow-400 */
--purple:        #c084fc;   /* purple-400 — nhãn phụ */
```

Điểm cần nhớ: nền **đen thuần**, không phải xám đen. Bề mặt tạo bằng lớp trắng mờ 2%, không phải màu nền riêng. Accent chính là **emerald**, không phải cyan.

### Chữ

```css
--font-display: Manrope, sans-serif;   /* heading */
--font-body:    Inter, sans-serif;     /* nội dung */
--font-mono:    ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
```

| Vai trò | Cỡ | Weight | Letter-spacing |
| --- | --- | --- | --- |
| Hero h1 | 96px | 600 | -4.8px (-0.05em) |
| Section h2 | 48px | 600 | -0.96px (-0.02em) |
| Card h3 | 24px | 600 | -0.48px (-0.02em) |
| Pricing h3 | 30px | 700 | -0.6px |
| Body | 16px | 400 | 0 |
| UI mặc định | 13px | 400 | 0 |
| Nhãn/badge | 11px | 400–500 | 0 |
| Micro | 9–10px | 400 | 0 |

Cỡ chữ chủ đạo trong UI là **13px** (440 lần xuất hiện) — đặc nhỏ, dày thông tin. Heading dùng letter-spacing âm mạnh, càng lớn càng âm.

### Hình khối, nhịp, chuyển động

```css
--r-sm: 4px;      /* mặc định, dùng nhiều nhất */
--r-md: 8px;
--r-lg: 12px;
--r-xl: 16px;
--r-full: 9999px; /* mọi button và pill */

/* nhịp dọc: section padding 128px (py-32), footer 80px/40px */
--ease: cubic-bezier(0.4, 0, 0.2, 1);
--ease-out: cubic-bezier(0.23, 1, 0.32, 1);
--t-fast: 0.15s;  --t-base: 0.3s;  --t-slow: 0.5s;
```

Button luôn bo tròn hoàn toàn. Card bo 4–12px. Đừng trộn ngược lại.

### Button

| Kiểu | Nền | Chữ | Padding | Viền |
| --- | --- | --- | --- | --- |
| Primary | trắng đặc | đen | 16px 40px | none |
| Secondary | `rgba(255,255,255,0.05)` | `#d4d4d8` | 16px 32px | 1px `rgba(255,255,255,0.1)` |
| Nav pill | `rgba(255,255,255,0.05)` | trắng | 8px 24px | none |
| Filter (off) | trong suốt | `#a3a3a3` | 6px 14px | 1px `rgba(255,255,255,0.1)` |
| Filter (on) | `rgba(239,35,60,0.1)` | `#ef233c` | 6px 14px | 1px `#ef233c` |

Tất cả `border-radius: 9999px`.

### Bố cục landing

Header `fixed`, `pt-24px`, trong suốt, `z-50`. Hero `min-h-screen`, padding trên 128px. Các section giữa đều `py-32 px-6`. Footer nền đen đặc, viền trên `zinc-900`.

---

## 8. Frontend

**Stack**: Next.js (App Router) + TypeScript + Tailwind + shadcn/ui. Tailwind biên dịch lúc build, không nhúng runtime như admin hiện tại.

Design token ở mục 7 khai báo trong `globals.css` dạng CSS variable, map vào `tailwind.config.ts`. Component đọc token, không hardcode mã màu.

```
napkey-web/
├── app/
│   └── [locale]/             # 'vi' | 'en'
│       ├── (marketing)/      # landing, pricing, docs
│       ├── (auth)/           # login, register, forgot-password
│       ├── console/          # khu vực cần đăng nhập
│       │   ├── page.tsx      # tổng quan usage
│       │   ├── keys/         # quản lý API key
│       │   ├── usage/        # bảng + biểu đồ chi tiết
│       │   ├── billing/      # nạp tiền, hoá đơn
│       │   └── settings/
│       └── api/              # BFF, giữ session cookie
├── messages/                 # vi.json, en.json
├── components/
│   ├── ui/                   # shadcn
│   └── napkey/               # component riêng
└── lib/
```

**Song ngữ Việt + Anh**: `next-intl` với route có tiền tố locale (`/vi/...`, `/en/...`). Tiếng Việt là mặc định, `/` redirect sang `/vi`. Middleware đọc `Accept-Language` cho lần ghé đầu, sau đó tôn trọng lựa chọn đã lưu trong cookie.

Ba điểm dễ bỏ sót khi làm song ngữ:

- **`hreflang` + canonical** cho từng trang marketing, nếu không hai bản dịch sẽ tự cạnh tranh nhau trên kết quả tìm kiếm.
- **Tiếng Việt dài hơn tiếng Anh khoảng 20–30%**. Với heading 96px letter-spacing âm thì nút và tiêu đề phải test ở cả hai ngôn ngữ, không chỉ bản Việt.
- **Không dịch định dạng số theo locale**: giá luôn hiển thị VNĐ ở cả hai bản (`1.500.000 ₫` / `1,500,000 ₫` theo quy ước từng ngôn ngữ, nhưng không đổi sang USD). Đổi đơn vị tiền theo ngôn ngữ là mời gọi tranh chấp thanh toán.

Nội dung kỹ thuật giữ nguyên gốc ở cả hai bản: `endpoint`, `streaming`, `token`, `API key`.

**Trang landing**, thứ tự dọc:

1. Hero — dòng chính 96px, hai CTA (primary trắng "Bắt đầu miễn phí", secondary "Xem tích hợp")
2. Ba thẻ giá trị — throughput, dự phòng nhiều tài khoản, một key mọi model
3. Đổi endpoint trong hai dòng code — khối code có nút copy
4. Bảng model + giá **VNĐ trên 1M token** (input / output / cache) — pill filter theo nhà cung cấp (dùng cặp màu danger ở mục 7)
5. Cách tính tiền — nạp trước, dùng bao nhiêu trừ bấy nhiêu, không gói tháng, không phí duy trì
6. Footer

Giá niêm yết theo **1M token** thay vì 1k: con số 1k token ra số lẻ nhỏ khó so sánh, còn mốc 1M là quy ước quen thuộc trong ngành. Nói rõ đơn vị `₫` cạnh mỗi số, và ghi rõ số dư nạp vào không rút lại được.

**Console**: sidebar dọc, mật độ thông tin cao theo cỡ chữ 13px. Bảng usage phân trang server-side. Biểu đồ dùng Recharts, một màu emerald, không gradient nhiều màu.

**Accessibility**: nền đen + `#525252` chỉ đạt ~3.4:1, chưa đủ AA cho body text — chỉ dùng `--text-dim` cho chữ trang trí cỡ lớn hoặc viền. Body text tối thiểu `#a3a3a3` (~9:1). Mọi control có focus ring thấy rõ; không truyền đạt trạng thái chỉ bằng màu (pill filter kèm nhãn/icon). Kiểm chứng đầy đủ WCAG cần test bằng screen reader thật và người rà soát chuyên môn — không tự nhận đạt chuẩn chỉ vì tỉ lệ tương phản.

---

## 9. Lộ trình

**Giai đoạn 0 — Dọn nền** (nhỏ, làm trước, không phụ thuộc gì)

- Chốt tiền tố key `nk_live_` / `nk_test_`, sửa `GenerateApiKeyValue()` cho khớp thực tế.
- Đổi mật khẩu admin khỏi `changeme` nếu còn mặc định; xác nhận `ADMIN_PASSWORD` đặt qua env ở môi trường thật.
- Bổ sung test cho đường đua hạn mức (mục 3.5) để có cái đối chiếu khi sửa.

**Giai đoạn 1 — Landing** (độc lập, ra mắt được ngay)

Dựng `napkey-web`, hoàn thiện trang marketing + tài liệu tích hợp. Chưa cần đăng nhập. Bán key thủ công qua admin panel. Đây là mốc có thể công khai sớm nhất.

**Giai đoạn 2 — Người dùng và key tự phục vụ**

Dựng `napkey-core` + Postgres. Đăng ký, xác minh email, tự tạo/thu hồi key. Key lưu dạng hash. `napkey-core` đồng bộ key sang `kiro-go`. Chưa có tiền — cấp hạn mức tay.

**Giai đoạn 3 — Sổ usage**

`kiro-go` báo usage về `napkey-core`. Bảng `usage_records` + `model_prices`. Console hiện usage thật. Vẫn chưa thu tiền, nhưng đã đối soát được — bước này phải đúng trước khi dính tới tiền.

**Giai đoạn 4 — Ví và nạp tiền qua Casso**

`wallets` + `ledger_entries` + `topup_orders` + `payment_events`. Luồng hold/settle ở mục 6, luồng nạp ở mục 6.1. Tự tắt key khi cạn ví. Đây là giai đoạn cần rà soát cẩn thận nhất.

Thứ tự trong giai đoạn này cũng quan trọng:

1. Liên kết tài khoản ngân hàng vào Casso, tạo tích hợp **Webhook V2**, lấy Key bảo mật.
2. Viết handler + worker, test bằng nút **Gọi thử** của Casso và lấy vector chữ ký thật làm fixture (xem cảnh báo ở mục 6.1).
3. Xác nhận Cloudflare không chặn Casso — whitelist IP theo hướng dẫn của họ.
4. Chạy job hậu kiểm trước khi mở cho khách, không phải sau.
5. Chuyển khoản thật một số tiền nhỏ để nghiệm thu đầu-cuối.

**Giai đoạn 5 — Vận hành**

Rate limit RPM/TPM. Phân quyền admin + audit log. Dashboard nội bộ: biên lợi nhuận, sức khoẻ pool, cảnh báo. Job đối chiếu số dư.

Thứ tự này có một nguyên tắc: **usage phải đo chính xác trước khi tính tiền, tính tiền phải đúng trước khi tự động hoá.**

---

## 10. Rủi ro bảo mật cần xử lý trước khi mở cho khách

Liệt kê thẳng, không giảm nhẹ:

1. **`config.json` chứa mật khẩu admin dạng thô** và key khách dạng thô. Đã được `.gitignore` che, nhưng ai đọc được file là nắm toàn quyền. Chuyển sang Postgres + hash là cách xử lý gốc.
2. **Đường đua hạn mức** (mục 3.5) — khách vượt hạn mức được. Nguy cơ tài chính trực tiếp.
3. **Ghi file mỗi request** — vừa nghẽn vừa dễ hỏng dữ liệu. Mất `config.json` là mất toàn bộ khách.
4. **Một mật khẩu admin, không audit** — không truy được ai đã làm gì.
5. **Chưa có rate limit thời gian** — một khách vắt cạn pool, ảnh hưởng mọi khách còn lại.
6. **Cần rà soát riêng**: rò rỉ dữ liệu giữa các tenant khi share pool upstream.
7. **Webhook nạp tiền là mặt tấn công trực tiếp vào ví.** Endpoint này public, không xác thực bằng session, và mỗi request hợp lệ làm tăng số dư. Yêu cầu tối thiểu: verify HMAC trên raw body bằng so sánh hằng thời gian, từ chối timestamp lệch quá 5 phút, `unique (provider, provider_tx_id)` ở tầng database, và ghi mọi payload kể cả bản bị từ chối. Chi tiết ở mục 6.1.
8. **Key bảo mật webhook và Casso API key là bí mật cấp cao** — chỉ đặt qua biến môi trường trong Coolify, không commit, không ghi vào log. Rò rỉ Key bảo mật đồng nghĩa với việc bất kỳ ai cũng tự nạp tiền vào ví của mình được.
9. **Mất Postgres là mất sổ tiền.** Backup định kỳ ra ngoài VPS, và phải thử restore thật một lần.

Mục 1–3 nên xong trước khi có khách trả tiền thật; mục 7–9 là điều kiện bắt buộc của Giai đoạn 4.

---

## 11. Quyết định đã chốt

| # | Hạng mục | Quyết định | Kéo theo |
| --- | --- | --- | --- |
| 1 | Thanh toán | **Casso Webhook V2** (chuyển khoản + tự đối soát) | Mục 6.1; bảng `topup_orders`, `payment_events` |
| 2 | Đơn vị tiền | **VNĐ**, lưu micro-VNĐ (`bigint`) | Giá theo 1M token; không dùng float |
| 3 | Mô hình bán | **Trả trước theo ví**, dùng bao nhiêu trừ bấy nhiêu | Không cần `subscriptions`; giữ luồng hold/settle mục 6 |
| 4 | Ngôn ngữ | **Việt + Anh**, `vi` mặc định | Route `[locale]`, `next-intl`, `hreflang` |
| 5 | Hạ tầng | **Coolify tự quản** tại `coolify.qbo.io.vn` | Postgres nội bộ, backup + restore thử, whitelist IP Casso qua Cloudflare |

Ba điểm còn mở, cần trả lời trước Giai đoạn 4 chứ không phải bây giờ:

- **Tỷ giá USD→VNĐ** dùng để định giá và biên lợi nhuận mong muốn trên mỗi model.
- **Tài khoản ngân hàng nhận tiền** — ngân hàng nào, cá nhân hay doanh nghiệp. Ảnh hưởng tới việc có dùng được tài khoản ảo (VA) thay cho `memo_code` không; VA sạch hơn hẳn về đối soát nhưng cần tài khoản doanh nghiệp ở ngân hàng có hỗ trợ.
- **Hoá đơn VAT** — có xuất hay không. Nếu có thì phát sinh thêm thông tin thuế trong `users` và một bảng hoá đơn.

Giai đoạn 0 và 1 không phụ thuộc vào ba điểm trên.
