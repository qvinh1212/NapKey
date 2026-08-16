# NapKey Design System — MASTER

> Source of truth cho toàn bộ giao diện napkey-web.
> Cảm hứng thiết kế: tokenrouter.com (trích xuất trực tiếp từ CSS production của họ, 2026-08-16).
> Sinh bởi ui-ux-pro-max (--design-system, variance 6 / motion 4 / density 4),
> sau đó override palette theo đúng yêu cầu "giống tokenrouter.com".

## 1. Style Direction

- **Phong cách**: Dark Mode cao cấp, developer-facing, tối giản nhưng có chiều sâu lớp (layered surfaces). Không phải đen tuyệt đối — nền than chì ấm #121317 để card nổi khối bằng border và surface tier.
- **Điểm nhấn duy nhất**: xanh điện #0086ff (electric blue). Một màu brand duy nhất cho CTA, link active, badge nổi. Không gradient rực rỡ; TokenRouter dùng màu phẳng + border tinh tế.
- **Không dùng**: light-mode mặc định, emoji làm icon, nhiều màu accent cùng lúc, glassmorphism nặng.

## 2. Color Tokens

| Token | Giá trị | Vai trò |
|---|---|---|
| `--color-bg` | `#121317` | Nền trang (thay #000000 hiện tại) |
| `--color-surface-1` | `#15171a` | Card, panel |
| `--color-surface-2` | `#1e2126` | Hover row, input, inset block |
| `--color-surface-3` | `#111214` | Footer, vùng chìm |
| `--color-line` | `#262a31` | Border, divider |
| `--color-fg` | `#f5f7fa` | Chữ chính |
| `--color-muted` | `#a1a7b3` | Chữ phụ, mô tả |
| `--color-dim` | `#6b7280` | Placeholder, metadata |
| `--color-brand` | `#0086ff` | CTA chính, link active, dot live |
| `--color-brand-hover` | `#0075ed` | Hover CTA |
| `--color-brand-light` | `#67b7ff` | Text link trên nền tối, badge |
| `--color-success` | `#34d399` | Trạng thái OK (giữ từ bảng cũ) |
| `--color-danger` | `#ef233c` | Lỗi, cảnh báo billing |
| `--color-warn` | `#facc15` | Cảnh báo nhẹ |

Contrast yêu cầu: chữ muted #a1a7b3 trên #121317 đạt ~7:1; brand #0086ff dùng cho chữ phải đi nền surface-2 trở lên hoặc dùng `--color-brand-light` thay thế.

## 3. Typography

- **Display**: Space Grotesk (Google Fonts, self-host qua next/font) — thế chỗ PP Neue Montreal của TokenRouter (font thương mại). Trọng số 500/700. Dùng cho H1-H3, metric value.
- **Body**: Inter (giữ nguyên, đã có next/font). Trọng số 400/500/600.
- **Mono**: Source Code Pro hoặc JetBrains Mono cho code block, API URL box, key preview.
- Thang chữ: hero 56-72px (clamp), section title 32-40px, body 16px, label/badge 12-13px uppercase letter-spacing 0.04em.

## 4. Shape & Spacing

- Pill badge: `border-radius: 999px` (badge hero, tag model, trạng thái).
- Card: 12px (nhỏ) / 16px (section card) / 20px (hero panel).
- Button: pill 999px cho CTA hero; 8-10px cho button trong bảng/form.
- Spacing scale (density 4): 8 / 12 / 16 / 24 / 32 / 48 / 80; section padding dọc 96-128px desktop.
- Border 1px `--color-line` trên mọi card; không shadow nặng — chiều sâu đến từ surface tier, shadow tối đa `0 8px 24px rgba(0,0,0,0.35)` cho dropdown/modal.

## 5. Motion (motion 4 — standard)

- Stagger list khi section vào viewport: mỗi item lệch 60-80ms, duration 300-450ms, ease `cubic-bezier(0.23, 1, 0.32, 1)` (giữ `--ease-out-expo` hiện có).
- Hover transition 150-250ms: đổi border-color về `--color-brand` hoặc nâng surface tier.
- Header sticky đổi trạng thái khi cuộn (--scrolled): thêm border-bottom + blur nền.
- Bắt buộc: `@media (prefers-reduced-motion: reduce)` tắt mọi translate/stagger.

## 6. Landing Structure (theo mẫu TokenRouter)

1. Header sticky: logo + nav + nút đăng nhập/đăng ký; trạng thái --scrolled.
2. Hero: badge pill + tagline có chấm brand + H1 + subtitle + hàng metric (label/value) + **URL box** `api.napkey.io.vn` với nút copy + CTA chính/phụ.
3. Logo wall / social proof (có thể thay bằng các client: Cursor, Claude Code, OpenAI SDK...).
4. Model access panel: danh sách 8 model theo MODEL_TIERS (pricing.ts) — mỗi hàng: model-name, tier badge, giá VND/1M, nút copy id.
5. Value props: card icon + tag + title + desc (tối đa 6).
6. OneAPI/compatibility: "1 API cho định dạng OpenAI / Claude" với code snippet.
7. Pricing detail (bảng giá + request shapes).
8. FAQ accordion.
9. Final CTA + footer cột link.

## 7. Quy tắc implementation (napkey-web)

- Toàn bộ màu đi qua token `@theme` trong globals.css; không hardcode hex trong component.
- Tailwind v4: dùng `bg-surface-1`, `border-line`, `text-muted`... từ token.
- Icon: SVG inline (component Icon hiện có), không emoji.
- next-intl: mọi文案 mới thêm key vào cả `vi.json` và `en.json`.
- Không thay đổi logic giá: chỉ render từ `src/lib/pricing.ts` (MODEL_TIERS, MODEL_PRICES, requestShapes, formatVnd).

## 8. Pre-Delivery Checklist

- [ ] Contrast 4.5:1 cho mọi cặp chữ/nền
- [ ] focus-visible ring `--color-brand-light` trên mọi phần tử tương tác
- [ ] cursor-pointer trên mọi phần tử bấm được
- [ ] prefers-reduced-motion respected
- [ ] Responsive 375 / 768 / 1024 / 1440 không tràn ngang
- [ ] Hover state 150-300ms trên card, button, row
- [ ] Không emoji làm icon; toàn bộ SVG
