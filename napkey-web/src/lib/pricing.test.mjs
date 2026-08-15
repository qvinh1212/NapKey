import assert from 'node:assert/strict';
import test from 'node:test';

import {
  creditsFromVnd,
  microcreditsFromVnd,
  MIN_TOPUP_VND,
  TOPUP_STEP_VND,
  TOPUP_PRESETS,
  VND_PER_CREDIT,
  VND_PER_REQUEST,
  MODEL_PRICES,
  MODEL_TIERS,
  FALLBACK_TOKEN_PRICE,
  tokenPriceForModel,
  requestCostVnd,
  requestsFromVnd,
  requestShapes,
} from './pricing.ts';

test('uses the public 400 VND per credit rate', () => {
  assert.equal(VND_PER_CREDIT, 400);
  assert.equal(creditsFromVnd(400_000), 1_000);
  assert.equal(microcreditsFromVnd(400_000), 1_000_000_000);
});

test('does not project a first-topup bonus', () => {
  assert.equal(creditsFromVnd(75_000), 187.5);
  assert.equal(creditsFromVnd(400_000), 1_000);
});

test('supports a 10,000 VND entry top-up and round-money presets', () => {
  assert.equal(MIN_TOPUP_VND, 10_000);
  assert.equal(TOPUP_STEP_VND, 1_000);
  assert.deepEqual(TOPUP_PRESETS, [10_000, 20_000, 40_000, 100_000, 200_000, 400_000]);
  assert.equal(creditsFromVnd(MIN_TOPUP_VND), 25);
  assert.equal(microcreditsFromVnd(MIN_TOPUP_VND), 25_000_000);
});

test('rejects invalid top-up projections', () => {
  assert.equal(creditsFromVnd(0), 0);
  assert.equal(creditsFromVnd(-60_000), 0);
  assert.equal(creditsFromVnd(Number.NaN), 0);
});

test('prices models at their tiered rates with a 300 VND per-request fee', () => {
  assert.equal(MODEL_PRICES['claude-sonnet-5'], 1_500);
  assert.equal(MODEL_PRICES['gpt-5.6-luna'], 1_500);
  assert.equal(MODEL_PRICES['claude-opus-4.7'], 3_000);
  assert.equal(MODEL_PRICES['claude-opus-4.8'], 3_000);
  assert.equal(MODEL_PRICES['gpt-5.6-terra'], 3_600);
  assert.equal(MODEL_PRICES['claude-opus-5'], 4_500);
  assert.equal(MODEL_PRICES['gpt-5.6-sol'], 6_000);
  // fable-5 has a price row (anchors the '*' fallback) but is not in the served
  // catalog because the pool key is not entitled to it upstream.
  assert.equal(MODEL_PRICES['claude-fable-5'], 10_000);
  assert.equal(FALLBACK_TOKEN_PRICE, 10_000);
  assert.equal(VND_PER_REQUEST, 300);
});

test('tokenPriceForModel is case-insensitive and falls back to the highest tier', () => {
  assert.equal(tokenPriceForModel('claude-sonnet-5'), 1_500);
  assert.equal(tokenPriceForModel('CLAUDE-SONNET-5'), 1_500);
  assert.equal(tokenPriceForModel('  Claude-Sonnet-5  '), 1_500);
  assert.equal(tokenPriceForModel('unknown-model-id'), FALLBACK_TOKEN_PRICE);
});

test('MODEL_TIERS is the served catalog: every model except fable-5, in ascending ratio', () => {
  // fable-5 is priced but not served (pool key not entitled to it upstream), so the
  // catalog has one fewer entry than the price book.
  assert.equal(MODEL_TIERS.length, Object.keys(MODEL_PRICES).length - 1);
  assert.ok(!MODEL_TIERS.some((tier) => tier.id === 'claude-fable-5'));
  for (const tier of MODEL_TIERS) {
    assert.ok(Object.prototype.hasOwnProperty.call(MODEL_PRICES, tier.id), `${tier.id} must be priced`);
  }
  for (let i = 1; i < MODEL_TIERS.length; i++) {
    assert.ok(MODEL_TIERS[i].ratio >= MODEL_TIERS[i - 1].ratio, 'tiers must be non-decreasing');
  }
});

test('requestCostVnd uses the model-specific rate when provided', () => {
  // Short chat on sonnet-5 (cheapest tier): fee dominates.
  const sonnetCost = requestCostVnd(2_600, 1_100, 'claude-sonnet-5');
  assert.equal(sonnetCost, 300 + (3_700 * 1_500) / 1_000_000);
  // Same shape on fable-5 (most expensive named tier).
  const fableCost = requestCostVnd(2_600, 1_100, 'claude-fable-5');
  assert.equal(fableCost, 300 + (3_700 * 10_000) / 1_000_000);
  assert.ok(fableCost > sonnetCost);
});

test('requestCostVnd without a model id falls back to the highest tier', () => {
  assert.equal(requestCostVnd(2_600, 1_100), requestCostVnd(2_600, 1_100, 'anything-unknown'));
});

test('requestsFromVnd reflects the chosen model', () => {
  const shape = requestShapes[0];
  const sonnetRequests = requestsFromVnd(10_000, shape, 'claude-sonnet-5');
  const fableRequests = requestsFromVnd(10_000, shape, 'claude-fable-5');
  assert.ok(sonnetRequests > fableRequests);
  assert.equal(requestsFromVnd(0, shape, 'claude-sonnet-5'), 0);
  assert.equal(requestsFromVnd(Number.NaN, shape, 'claude-sonnet-5'), 0);
});
