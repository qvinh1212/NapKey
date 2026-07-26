# napkey-web

Landing song ngu (vi/en) cho NapKey — cong ban quyen truy cap API Claude
tra truoc theo vi, tinh bang VND.

## Chay

```bash
npm install
npm run dev     # http://localhost:3000 -> redirect /vi
```

## Kiem tra

```bash
npm run typecheck
npm run lint
npm run build
```

## Bien moi truong

Sao chep `.env.example` sang `.env.local` roi dieu chinh:

| Bien | Y nghia |
| --- | --- |
| `NEXT_PUBLIC_SITE_URL` | Base URL dung cho metadata, canonical, sitemap |
| `NEXT_PUBLIC_API_BASE_URL` | Endpoint API cong khai hien trong cac doan code mau |

## Cau truc

```
src/
├── app/[locale]/          # route song ngu, vi la mac dinh
├── components/
│   ├── napkey/            # header, footer, logo, locale switcher
│   ├── sections/          # 6 section cua landing
│   └── ui/                # button, card, section
├── i18n/                  # cau hinh next-intl
└── lib/                   # bang gia, snippet, hang so site
messages/                  # vi.json, en.json
```

## Ghi chu ky thuat

- **Gia** trong `src/lib/pricing.ts` dan xuat tu gia goc Anthropic (USD/MTok)
  qua hang so `VND_PER_USD_BILLED`. Doi gia thi sua hang so do, dong thoi ghi
  lai thoi diem hieu luc trong bang `model_prices` (xem `DESIGN.md` muc 5).
- **Font** self-host trong `src/app/fonts/` va nap qua `next/font/local`.
  Khong dung `next/font/google` vi buoc build se goi `fonts.googleapis.com`,
  lam CI va Docker build hong khi mang bi chan. Chi giu subset latin,
  latin-ext va vietnamese.
- **Design token** trong `src/app/globals.css` trich tu computed style cua
  trang tham chieu, khong phai uoc luong. Doc `DESIGN.md` muc 7 truoc khi doi.
- **Tien te** luon hien thi VND o ca hai ban ngon ngu. Doi don vi tien theo
  ngon ngu la moi goi tranh chap thanh toan.
- File nay la Giai doan 1 trong lo trinh o `DESIGN.md` muc 9: chi co trang
  marketing, chua co dang nhap va chua noi thanh toan.
