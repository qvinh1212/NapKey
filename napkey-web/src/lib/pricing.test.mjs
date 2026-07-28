import assert from 'node:assert/strict';
import test from 'node:test';

import { creditsFromVnd, microcreditsFromVnd, VND_PER_CREDIT } from './pricing.ts';

test('uses the public 60 VND per credit rate', () => {
  assert.equal(VND_PER_CREDIT, 60);
  assert.equal(creditsFromVnd(60_000), 1_000);
  assert.equal(microcreditsFromVnd(60_000), 1_000_000_000);
});

test('rejects invalid top-up projections', () => {
  assert.equal(creditsFromVnd(0), 0);
  assert.equal(creditsFromVnd(-60_000), 0);
  assert.equal(creditsFromVnd(Number.NaN), 0);
});
