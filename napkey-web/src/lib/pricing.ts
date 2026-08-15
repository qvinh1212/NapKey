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

// Per-request fee, unchanged since migration 0019. Recovers the fixed upstream call
// cost that token rates cannot see. The same across every tier because the upstream
// charges roughly one credit per call regardless of which model answers.
export const VND_PER_REQUEST = 300;

/**
 * Tiered token prices from migration 0021. Each model carries its own rate rather
 * than sharing a flat one: the upstream cost spans 6x across the catalog, and a
 * single rate either overcharges cheap models or undercharges expensive ones.
 *
 * Values are VND per million tokens. Upstream costs in parentheses come from the
 * ratio-based measurement on the live key (1000 credits = 50,000 VND).
 */
export const MODEL_PRICES: Record<string, number> = {
  'claude-sonnet-5': 1_500,
  'gpt-5.6-luna': 1_500,
  'claude-opus-4.7': 3_000,
  'claude-opus-4.8': 3_000,
  'gpt-5.6-terra': 3_600,
  'claude-opus-5': 4_500,
  'gpt-5.6-sol': 6_000,
  'claude-fable-5': 10_000,
} as const;

/** Fallback rate for any model id not explicitly listed above. Matches fable-5. */
export const FALLBACK_TOKEN_PRICE = 10_000;

/**
 * Upstream cost basis per model, VND per million tokens, from the ratio-based
 * measurement on the live key (2,097 VND/1M x ratio). Kept next to the retail
 * table so the margin stays auditable in one place.
 */
export const UPSTREAM_MODEL_COSTS: Record<string, number> = {
  'claude-sonnet-5': 1_049,
  'gpt-5.6-luna': 1_049,
  'claude-opus-4.7': 2_097,
  'claude-opus-4.8': 2_097,
  'gpt-5.6-terra': 2_516,
  'claude-opus-5': 3_146,
  'gpt-5.6-sol': 4_194,
  'claude-fable-5': 6_920,
} as const;

/**
 * The served catalog in price order, cheapest tier first. The ratio is the
 * upstream-credit multiplier relative to the 1x baseline; the pricing page shows
 * the model and its tier, and leaves the VND conversion out of sight because
 * customers settle in wallet credits.
 *
 * claude-fable-5 has a price row in migration 0021 but is not listed here: the
 * pool key is not entitled to it upstream, so it was withdrawn from sale in the
 * same change that added this table. Its rate survives only to anchor the '*'
 * fallback at the top tier.
 */
export const MODEL_TIERS: readonly { id: string; ratio: number }[] = [
  { id: 'claude-sonnet-5', ratio: 0.5 },
  { id: 'gpt-5.6-luna', ratio: 0.5 },
  { id: 'claude-opus-4.7', ratio: 1 },
  { id: 'claude-opus-4.8', ratio: 1 },
  { id: 'gpt-5.6-terra', ratio: 1.2 },
  { id: 'claude-opus-5', ratio: 1.5 },
  { id: 'gpt-5.6-sol', ratio: 2 },
] as const;

/** Look up the VND/1M rate for a given model id. Case-insensitive. */
export function tokenPriceForModel(modelId: string): number {
  const normalized = modelId.trim().toLowerCase();
  for (const [key, price] of Object.entries(MODEL_PRICES)) {
    if (key.toLowerCase() === normalized) return price;
  }
  return FALLBACK_TOKEN_PRICE;
}

/** What one request costs in VND, given its model and token count. Mirrors pricing.Compute in Go. */
export function requestCostVnd(inputTokens: number, outputTokens: number, modelId?: string): number {
  const tokens = Math.max(0, inputTokens) + Math.max(0, outputTokens);
  const rate = modelId ? tokenPriceForModel(modelId) : FALLBACK_TOKEN_PRICE;
  return VND_PER_REQUEST + (tokens * rate) / 1_000_000;
}

/**
 * Roughly how many requests a top-up buys. Deliberately a range, not one number: the
 * same money buys very different amounts of work depending on the model and shape,
 * and quoting a single figure would set an expectation the traffic cannot keep.
 */
export function requestsFromVnd(vnd: number, shape: RequestShape, modelId?: string): number {
  if (!Number.isFinite(vnd) || vnd <= 0) return 0;
  return Math.floor(vnd / requestCostVnd(shape.inputTokens, shape.outputTokens, modelId));
}

export type RequestShape = { key: string; inputTokens: number; outputTokens: number };

// Measured against the live upstream on 2026-08-06, including the ~2,600 tokens it
// injects into every prompt. These are the shapes customers actually send, so the
// pricing page can answer "what will this cost me" with observed numbers.
export const requestShapes: readonly RequestShape[] = [
  { key: 'chat', inputTokens: 2_600, outputTokens: 1_100 },
  { key: 'agentStep', inputTokens: 4_100, outputTokens: 1_400 },
  { key: 'largeContext', inputTokens: 52_600, outputTokens: 1_400 },
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
