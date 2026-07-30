import assert from 'node:assert/strict';
import test from 'node:test';

import {
  creditsFromVnd,
  microcreditsFromVnd,
  MIN_TOPUP_VND,
  TOPUP_STEP_VND,
  TOPUP_PRESETS,
  VND_PER_CREDIT,
} from './pricing.ts';

test('uses the public 75 VND per credit rate', () => {
  assert.equal(VND_PER_CREDIT, 75);
  assert.equal(creditsFromVnd(75_000), 1_000);
  assert.equal(microcreditsFromVnd(75_000), 1_000_000_000);
});

test('supports a 10,000 VND entry top-up and round-money presets', () => {
  assert.equal(MIN_TOPUP_VND, 10_000);
  assert.equal(TOPUP_STEP_VND, 1_000);
  assert.deepEqual(TOPUP_PRESETS, [10_000, 30_000, 75_000, 150_000, 375_000]);
  assert.equal(creditsFromVnd(MIN_TOPUP_VND), 10_000 / 75);
  assert.equal(microcreditsFromVnd(MIN_TOPUP_VND), 133_333_333);
});

test('rejects invalid top-up projections', () => {
  assert.equal(creditsFromVnd(0), 0);
  assert.equal(creditsFromVnd(-60_000), 0);
  assert.equal(creditsFromVnd(Number.NaN), 0);
});
