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

test('uses the public 400 VND per credit rate and 70 percent gross margin', () => {
  assert.equal(VND_PER_CREDIT, 400);
  assert.equal(creditsFromVnd(400_000), 1_000);
  assert.equal(microcreditsFromVnd(400_000), 1_000_000_000);
  assert.ok((400 - 110) / 400 >= 0.70);
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
