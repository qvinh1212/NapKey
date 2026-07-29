import assert from 'node:assert/strict';
import test from 'node:test';
import { usageRecordView } from './usage-record-view.ts';

test('maps usage records into the compact ledger columns', () => {
  const view = usageRecordView({
    status: 'success',
    estimated: true,
    unpriced: false,
    tokens: { input: 10, output: 20, cacheRead: 30, cacheWrite: 40, total: 100 },
  });

  assert.deepEqual(view, {
    type: 'chat',
    status: 'success',
    statusTone: 'accent',
    cacheTokens: 70,
    quality: 'estimated',
  });
});

test('uses danger status for failed requests and exposes unpriced quality', () => {
  const view = usageRecordView({
    status: 'error',
    estimated: false,
    unpriced: true,
    tokens: { input: 0, output: 0, cacheRead: 0, cacheWrite: 0, total: 0 },
  });

  assert.equal(view.statusTone, 'danger');
  assert.equal(view.quality, 'unpriced');
});
