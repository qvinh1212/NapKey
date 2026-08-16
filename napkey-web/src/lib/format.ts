import type { Credits, Money, TokenBreakdown } from '@/lib/api/types';

/**
 * Ham dinh dang dung chung cho console.
 *
 * Dinh dang theo don vi Credit (1 Credit = 75 VND theo Cach 2 - Margin 1.5x).
 */

/** Hien mot so tien theo don vi Credit (1 Credit = 75 VND). */
export function money(value: Money | undefined | null, locale = 'vi'): string {
  if (!value) return '0 CR';
  const cr = value.vnd / 75;
  const formatted = new Intl.NumberFormat(locale === 'vi' ? 'vi-VN' : 'en-US', {
    maximumFractionDigits: cr % 1 === 0 ? 0 : 2,
  }).format(cr);
  return `${formatted} CR`;
}

/** Dinh dang so tien VND goc neu can hien phu de */
export function moneyVnd(value: Money | undefined | null): string {
  if (!value) return '0 ₫';
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
    maximumFractionDigits: 2,
  }).format(value?.credits ?? 0)} CR`;
}

/** Rut gon so lon cho the thong ke: 1.2M, 45.3K. */
export function compact(value: number, locale: string): string {
  return new Intl.NumberFormat(locale === 'vi' ? 'vi-VN' : 'en-US', {
    notation: 'compact',
    maximumFractionDigits: 1,
  }).format(value);
}

/** Do tre (milliseconds hoac seconds). */
export function latency(ms: number | undefined | null, locale: string): string {
  if (!ms || ms <= 0) return '—';
  if (ms < 1000) return `${count(ms, locale)} ms`;
  return `${new Intl.NumberFormat(locale === 'vi' ? 'vi-VN' : 'en-US', {
    maximumFractionDigits: 2,
  }).format(ms / 1000)} s`;
}

/** Tom tat token breakdown: input + output. */
export function tokens(breakdown: TokenBreakdown | undefined | null, locale: string): string {
  if (!breakdown) return '0';
  return count(breakdown.total, locale);
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
 * napkey-core cat ngay theo gio Viet Nam (UTC+7). Trinh duyet o mui gio khac phai
 * hien dung moc do, neu khong cot "hom nay" tren bieu do se lech so voi so ledger.
 */
export const BILLING_TIME_ZONE = 'Asia/Ho_Chi_Minh';

/**
 * Khoang thoi gian tinh tien theo ISO string.
 *
 * Tra ve { from, to } de nap vao URL detail / ledger. Dung chung mot ham de hai man
 * hinh khong bao gio lech nhau mot ngay do tinh `now - N days` o hai thoi diem.
 */
export function billingRange(days: number): { from: string; to: string } {
  const now = new Date();
  const from = new Date(now.getTime() - days * 24 * 60 * 60 * 1000);
  return {
    from: from.toISOString(),
    to: now.toISOString(),
  };
}
