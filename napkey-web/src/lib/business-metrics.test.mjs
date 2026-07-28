import assert from 'node:assert/strict';
import test from 'node:test';
import { businessRates } from './business-metrics.ts';

test('calculates acquisition, activation, payment and repeat rates', () => {
  assert.deepEqual(businessRates({ newUsers: 20, verifiedUsers: 15, activatedUsers: 8, newPayingUsers: 5, payingCustomers: 5, repeatCustomers: 2 }), {
    verification: 75,
    activation: 40,
    payment: 25,
    repeat: 40,
  });
});

test('returns zero instead of NaN when a funnel has no denominator', () => {
  assert.deepEqual(businessRates({ newUsers: 0, verifiedUsers: 0, activatedUsers: 0, newPayingUsers: 0, payingCustomers: 0, repeatCustomers: 0 }), {
    verification: 0,
    activation: 0,
    payment: 0,
    repeat: 0,
  });
});
