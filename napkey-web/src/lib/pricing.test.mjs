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

test('uses the public 60 VND per credit rate', () => {
  assert.equal(VND_PER_CREDIT, 60);
  assert.equal(creditsFromVnd(60_000), 1_000);
  assert.equal(microcreditsFromVnd(60_000), 1_000_000_000);
});

test('supports a 10,000 VND entry top-up and round-money presets', () => {
  assert.equal(MIN_TOPUP_VND, 10_000);
  assert.equal(TOPUP_STEP_VND, 1_000);
  assert.deepEqual(TOPUP_PRESETS, [10_000, 30_000, 60_000, 120_000, 300_000]);
  assert.equal(creditsFromVnd(MIN_TOPUP_VND), 10_000 / 60);
  assert.equal(microcreditsFromVnd(MIN_TOPUP_VND), 166_666_666);
});

test('rejects invalid top-up projections', () => {
  assert.equal(creditsFromVnd(0), 0);
  assert.equal(creditsFromVnd(-60_000), 0);
  assert.equal(creditsFromVnd(Number.NaN), 0);
});
