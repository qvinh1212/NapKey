import assert from 'node:assert/strict';
import test from 'node:test';
import { normalizePublicStatus } from './public-status.ts';

test('normalizes a healthy data-plane response without exposing extra fields', () => {
  assert.deepEqual(
    normalizePublicStatus({ status: 'ok', uptime: 125, version: '1.1.5', accounts: 4 }),
    { operational: true, version: '1.1.5', uptimeSeconds: 125 },
  );
});

test('rejects malformed or unhealthy responses', () => {
  assert.deepEqual(normalizePublicStatus({ status: 'down', uptime: -1, version: 7 }), {
    operational: false,
    version: '',
    uptimeSeconds: 0,
  });
  assert.deepEqual(normalizePublicStatus(null), {
    operational: false,
    version: '',
    uptimeSeconds: 0,
  });
});
