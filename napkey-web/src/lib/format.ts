import type { Credits, Money, TokenBreakdown } from '@/lib/api/types';

/**
 * Ham dinh dang dung chung cho console.
 *
 * Nguyen tac: tien thi HIEN theo `formatted` backend gui san, khong tu tinh lai.
 * Hai noi tu lam tron doc lap la hai noi se lech nhau mot dong, va khach se thay.
 */

/** Hien mot so tien. Uu tien ban da dinh dang tu backend. */
export function money(value: Money | undefined | null): string {
  if (!value) return '0 \u20ab';
  return value.formatted;
}

/**
 * Dinh dang so nguyen theo locale.
 *
 * Token dem bang don vi nghin/trieu nen dau phan cach la bat buoc de doc duoc:
 * "1234567" va "1.234.567" la hai muc do de hieu khac nhau.
 */
export function count(value: number, locale: string): string {
  return new Intl.NumberFormat(locale === 'vi' ? 'vi-VN' : 'en-US').format(value);
}

export function creditAmount(value: Credits | undefined | null, locale: string): string {
  return `${new Intl.NumberFormat(locale === 'vi' ? 'vi-VN' : 'en-US', {
    maximumFractionDigits: 4,
  }).format(value?.credits ?? 0)} credit`;
}

/** Rut gon so lon cho the thong ke: 1.2M, 45.3K. */
export function compact(value: number, locale: string): string {
  return new Intl.NumberFormat(locale === 'vi' ? 'vi-VN' : 'en-US', {
    notation: 'compact',
    maximumFractionDigits: 1,
  }).format(value);
}

/** Ngay + gio, theo mui gio Viet Nam de khop moc ngay ma backend cat. */
export function dateTime(iso: string, locale: string): string {
  return new Intl.DateTimeFormat(locale === 'vi' ? 'vi-VN' : 'en-US', {
    dateStyle: 'short',
    timeStyle: 'short',
    timeZone: BILLING_TIME_ZONE,
  }).format(new Date(iso));
}

/** Chi ngay, dung cho truc chart. */
export function dayLabel(iso: string, locale: string): string {
  return new Intl.DateTimeFormat(locale === 'vi' ? 'vi-VN' : 'en-US', {
    day: '2-digit',
    month: '2-digit',
    timeZone: BILLING_TIME_ZONE,
  }).format(new Date(`${iso}T00:00:00+07:00`));
}

/**
 * Mui gio tinh tien.
 *
 * Phai khop `billingTimeZone` trong napkey-core: backend cat moc ngay theo gio Ha
 * Noi, nen console hien theo UTC se lam tong mot ngay khong khop voi bieu do.
 */
export const BILLING_TIME_ZONE = 'Asia/Ho_Chi_Minh';

/**
 * Ngay lech `offsetDays` so voi hom nay, dang YYYY-MM-DD theo gio tinh tien.
 *
 * `en-CA` cho ra dung dinh dang ISO (2026-01-31), la dinh dang backend nhan.
 * Cong/tru ngay bang UTC roi moi dinh dang theo gio Ha Noi: vi buoc nhay luon la
 * boi so cua 24 gio, ngay dia phuong dich dung bang so ngay da cong.
 */
function billingDate(offsetDays: number): string {
  const at = new Date();
  at.setUTCDate(at.getUTCDate() + offsetDays);
  return new Intl.DateTimeFormat('en-CA', {
    timeZone: BILLING_TIME_ZONE,
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
  }).format(at);
}

/**
 * Khoang thoi gian `days` ngay gan nhat, TINH CA hom nay.
 *
 * Diem de sai: backend coi khoang la nua mo - `from` tinh vao, `to` KHONG tinh vao
 * (xem `UsageRange` trong napkey-core). Mot ngay tran khi phan tich se thanh nua dem
 * dau ngay do. Nen `to` phai la NGAY MAI, neu khong toan bo traffic hom nay bi loai
 * khoi ket qua va khach se thay so lieu dung mot ngay truoc.
 */
export function billingRange(days: number): { from: string; to: string } {
  return { from: billingDate(-(days - 1)), to: billingDate(1) };
}

/** Tong token, dung khi chi can mot con so. */
export function totalTokens(tokens: TokenBreakdown | undefined): number {
  return tokens?.total ?? 0;
}

/** Do tre, lam tron ve ms hoac giay tuy do lon. */
export function latency(ms: number | undefined, locale: string): string {
  if (ms === undefined) return '\u2014';
  if (ms < 1000) return `${count(ms, locale)} ms`;
  return `${(ms / 1000).toFixed(1)} s`;
}
