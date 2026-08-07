import assert from 'node:assert/strict';
import test from 'node:test';

import { spendMeter } from './spend-meter.ts';

const VND = 1_000_000;

test('measures lifetime spend against spend plus the remaining balance', () => {
  assert.deepEqual(spendMeter(2_868 * VND, 2_132 * VND, 0), {
    used: 2_868 * VND,
    balance: 2_132 * VND,
    available: 2_132 * VND,
    held: 0,
    total: 5_000 * VND,
    percent: 57.36,
    tone: 'accent',
  });
});

test('keeps held money in the balance but out of the available amount', () => {
  const meter = spendMeter(900 * VND, 100 * VND, 25 * VND);

  assert.equal(meter.total, 1_000 * VND);
  assert.equal(meter.available, 75 * VND);
  assert.equal(meter.percent, 90);
  assert.equal(meter.tone, 'danger');
});

test('handles empty and invalid counters without producing NaN', () => {
  assert.deepEqual(spendMeter(Number.NaN, -10, Number.POSITIVE_INFINITY), {
    used: 0,
    balance: 0,
    available: 0,
    held: 0,
    total: 0,
    percent: 0,
    tone: 'accent',
  });
});

test('warns once at least 70 percent of the funded amount is spent', () => {
  assert.equal(spendMeter(699 * VND, 301 * VND, 0).tone, 'accent');
  assert.equal(spendMeter(700 * VND, 300 * VND, 0).tone, 'warn');
  assert.equal(spendMeter(900 * VND, 100 * VND, 0).tone, 'danger');
});

// The bug this rename exists to prevent: fed credit counts, which are zero on every
// 9Router request, the bar reported an empty meter while real money drained away.
test('a wallet with real spend never reports an empty meter', () => {
  const meter = spendMeter(330 * VND, 9_670 * VND, 0);

  assert.ok(meter.percent > 0, 'spend must move the bar');
  assert.equal(meter.used, 330 * VND);
});
