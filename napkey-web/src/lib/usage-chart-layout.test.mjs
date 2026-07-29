import assert from 'node:assert/strict';
import test from 'node:test';
import { usageBarPercent } from './usage-chart-layout.ts';

test('scales horizontal usage bars against the largest day', () => {
  assert.equal(usageBarPercent(100, 100), 100);
  assert.equal(usageBarPercent(50, 100), 50);
});

test('keeps small non-zero usage visible and clamps malformed values', () => {
  assert.equal(usageBarPercent(1, 1000), 2);
  assert.equal(usageBarPercent(0, 1000), 0);
  assert.equal(usageBarPercent(2000, 1000), 100);
});
