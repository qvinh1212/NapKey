/**
 * Bang gia ban cua NapKey.
 *
 * Cach dan xuat gia, de sau nay con doi soat duoc:
 *  1. Gia goc Anthropic niem yet bang USD tren 1M token (MTok).
 *  2. `VND_PER_USD_BILLED` la mot HANG SO VAN HANH: da gop ty gia
 *     va bien lai vao mot so duy nhat. KHONG goi API ty gia luc
 *     tinh tien - gia phai on dinh va tra cuu duoc.
 *  3. Con so nay nho hon ty gia thi truong, tuc gia ban cua NapKey
 *     thap hon gia mua truc tiep tu Anthropic. Do la ly do ton tai
 *     cua dich vu.
 *
 * Khi doi gia: cap nhat hang so, ghi lai thoi diem hieu luc trong
 * `model_prices` (DESIGN.md muc 5). Usage da ghi khong tinh lai.
 */
const VND_PER_USD_BILLED = 18_000;

const usdPerMTok = (usd: number) => Math.round(usd * VND_PER_USD_BILLED);

export const modelFamilies = ['opus', 'sonnet', 'haiku'] as const;
export type ModelFamily = (typeof modelFamilies)[number];

export type PriceTag = 'strongest' | 'balanced' | 'fastest';

export type ModelPrice = {
  /** ID goi thang vao API, khop voi /v1/models cua data plane. */
  id: string;
  label: string;
  family: ModelFamily;
  tag?: PriceTag;
  /** Co bien the `-thinking` dung chung muc gia. */
  hasThinking: boolean;
  /** Tat ca don vi la dong tren 1 trieu token. */
  inputVnd: number;
  outputVnd: number;
  cacheReadVnd: number;
  cacheWriteVnd: number;
};

/** Gia goc Anthropic (USD/MTok) theo tung bac model. */
const tiers = {
  opus: { input: 5, output: 25, cacheRead: 0.5, cacheWrite: 6.25 },
  sonnet: { input: 3, output: 15, cacheRead: 0.3, cacheWrite: 3.75 },
  haiku: { input: 1, output: 5, cacheRead: 0.1, cacheWrite: 1.25 },
} as const;

type ModelSeed = {
  id: string;
  label: string;
  family: ModelFamily;
  tag?: PriceTag;
  hasThinking?: boolean;
};

const seeds: readonly ModelSeed[] = [
  { id: 'claude-opus-4.7', label: 'Claude Opus 4.7', family: 'opus', tag: 'strongest' },
  { id: 'claude-opus-4.6', label: 'Claude Opus 4.6', family: 'opus' },
  { id: 'claude-opus-4.5', label: 'Claude Opus 4.5', family: 'opus' },
  { id: 'claude-sonnet-4.6', label: 'Claude Sonnet 4.6', family: 'sonnet', tag: 'balanced' },
  { id: 'claude-sonnet-4.5', label: 'Claude Sonnet 4.5', family: 'sonnet' },
  { id: 'claude-sonnet-4', label: 'Claude Sonnet 4', family: 'sonnet' },
  { id: 'claude-haiku-4.5', label: 'Claude Haiku 4.5', family: 'haiku', tag: 'fastest' },
];

export const modelPrices: readonly ModelPrice[] = seeds.map((seed) => {
  const tier = tiers[seed.family];
  return {
    ...seed,
    hasThinking: seed.hasThinking ?? true,
    inputVnd: usdPerMTok(tier.input),
    outputVnd: usdPerMTok(tier.output),
    cacheReadVnd: usdPerMTok(tier.cacheRead),
    cacheWriteVnd: usdPerMTok(tier.cacheWrite),
  };
});

/**
 * Dinh dang tien theo locale, luon giu don vi VND o ca hai ban ngon ngu.
 * Doi don vi tien theo ngon ngu la moi goi tranh chap thanh toan.
 */
export function formatVnd(value: number, locale: string): string {
  return new Intl.NumberFormat(locale === 'en' ? 'en-US' : 'vi-VN', {
    maximumFractionDigits: 0,
  }).format(value);
}
