import assert from 'node:assert/strict';
import test from 'node:test';

import { creditMeter } from './credit-meter.ts';

test('measures lifetime usage against used credits plus the wallet balance', () => {
  assert.deepEqual(creditMeter(2_868, 2_132, 0), {
    used: 2_868,
    balance: 2_132,
    available: 2_132,
    held: 0,
    total: 5_000,
    percent: 57.36,
    tone: 'accent',
  });
});

test('keeps held credits in the balance but out of the available amount', () => {
  const meter = creditMeter(900, 100, 25);

  assert.equal(meter.total, 1_000);
  assert.equal(meter.available, 75);
  assert.equal(meter.percent, 90);
  assert.equal(meter.tone, 'danger');
});

test('handles empty and invalid counters without producing NaN', () => {
  assert.deepEqual(creditMeter(Number.NaN, -10, Number.POSITIVE_INFINITY), {
    used: 0,
    balance: 0,
    available: 0,
    held: 0,
    total: 0,
    percent: 0,
    tone: 'accent',
  });
});

test('warns once at least 70 percent of purchased credits are used', () => {
  assert.equal(creditMeter(699, 301, 0).tone, 'accent');
  assert.equal(creditMeter(700, 300, 0).tone, 'warn');
  assert.equal(creditMeter(900, 100, 0).tone, 'danger');
});
