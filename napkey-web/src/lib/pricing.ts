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

// The token price customers are actually charged, from migration 0019. Credits are an
// internal unit that only the Kiro upstream reports; the 9Router upstream that serves
// customer traffic speaks the OpenAI protocol, which carries no credit meter, so
// napkey-core prices those requests from token counts alone. Quoting a credit rate on
// the pricing page therefore described a mechanism no customer request goes through.
export const VND_PER_MILLION_TOKENS = 12_000;

// Charged once per request. The upstream bills roughly a fixed amount per call no
// matter how small it is, and a token-only price cannot see that cost: a 1,800-token
// request earns ~22 VND of token revenue against ~110 VND of cost. This is the larger
// part of a short request's bill, so hiding it invites exactly the dispute it caused.
export const VND_PER_REQUEST = 300;

/** What one request costs, given its token count. Mirrors pricing.Compute in Go. */
export function requestCostVnd(inputTokens: number, outputTokens: number): number {
  const tokens = Math.max(0, inputTokens) + Math.max(0, outputTokens);
  return VND_PER_REQUEST + (tokens * VND_PER_MILLION_TOKENS) / 1_000_000;
}

/**
 * Roughly how many requests a top-up buys. Deliberately a range, not one number: the
 * same money buys ~29 short chats or ~10 large-context calls, and quoting a single
 * figure would set an expectation the traffic cannot keep.
 */
export function requestsFromVnd(vnd: number, shape: RequestShape): number {
  if (!Number.isFinite(vnd) || vnd <= 0) return 0;
  return Math.floor(vnd / requestCostVnd(shape.inputTokens, shape.outputTokens));
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
