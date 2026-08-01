export const VND_PER_CREDIT = 400;
export const MICROCREDITS_PER_CREDIT = 1_000_000;
export const MIN_TOPUP_VND = 10_000;
export const TOPUP_STEP_VND = 1_000;
export const TOPUP_PRESETS = [10_000, 20_000, 40_000, 100_000, 200_000, 400_000] as const;

export const creditPackages = [
  { credits: 250, vnd: 100_000, key: 'starter' },
  { credits: 500, vnd: 200_000, key: 'builder' },
  { credits: 1_000, vnd: 400_000, key: 'scale' },
  { credits: 2_000, vnd: 800_000, key: 'pro' },
] as const;

/** Convert a top-up amount to the exact customer credit projection. */
export function creditsFromVnd(vnd: number): number {
  if (!Number.isFinite(vnd) || vnd <= 0) return 0;
  return vnd / VND_PER_CREDIT;
}

export function microcreditsFromVnd(vnd: number): number {
  return Math.floor(creditsFromVnd(vnd) * MICROCREDITS_PER_CREDIT);
}

export function formatVnd(value: number, locale: string): string {
  return `${new Intl.NumberFormat(locale === 'en' ? 'en-US' : 'vi-VN', {
    maximumFractionDigits: 0,
  }).format(value)} VND`;
}
