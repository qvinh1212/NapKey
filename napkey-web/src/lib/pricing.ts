export const VND_PER_CREDIT = 75;
export const MICROCREDITS_PER_CREDIT = 1_000_000;
export const MIN_TOPUP_VND = 10_000;
export const TOPUP_STEP_VND = 1_000;
export const TOPUP_PRESETS = [10_000, 30_000, 75_000, 150_000, 375_000] as const;
export const FIRST_TOPUP_BONUS_CAP_CREDITS = 1_000;
export const FIRST_TOPUP_BONUS_MIN_VND = 75_000;

export const creditPackages = [
  { credits: 1_000, vnd: 75_000, key: 'starter' },
  { credits: 5_000, vnd: 375_000, key: 'builder' },
  { credits: 10_000, vnd: 750_000, key: 'scale' },
] as const;

/** Convert a top-up amount to the exact customer credit projection. */
export function creditsFromVnd(vnd: number): number {
  if (!Number.isFinite(vnd) || vnd <= 0) return 0;
  return vnd / VND_PER_CREDIT;
}

export function microcreditsFromVnd(vnd: number): number {
  return Math.floor(creditsFromVnd(vnd) * MICROCREDITS_PER_CREDIT);
}

export function firstTopupBonusCredits(vnd: number): number {
  if (!Number.isFinite(vnd) || vnd < FIRST_TOPUP_BONUS_MIN_VND) return 0;
  return Math.min(creditsFromVnd(vnd), FIRST_TOPUP_BONUS_CAP_CREDITS);
}

export function formatVnd(value: number, locale: string): string {
  return `${new Intl.NumberFormat(locale === 'en' ? 'en-US' : 'vi-VN', {
    maximumFractionDigits: 0,
  }).format(value)} VND`;
}
