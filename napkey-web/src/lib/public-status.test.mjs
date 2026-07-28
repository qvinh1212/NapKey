import assert from 'node:assert/strict';
import test from 'node:test';
import { normalizePublicStatus } from './public-status.ts';

test('normalizes an operational service response without exposing extra fields', () => {
  assert.deepEqual(
    normalizePublicStatus({
      status: 'operational', checkedAt: '2026-07-28T15:00:00Z',
      components: [
        { id: 'gateway', status: 'operational' },
        { id: 'billing', status: 'operational' },
        { id: 'usage', status: 'operational' },
      ],
      internal: 'ignored',
    }),
    {
      status: 'operational',
      checkedAt: '2026-07-28T15:00:00Z',
      components: [
        { id: 'gateway', status: 'operational' },
        { id: 'billing', status: 'operational' },
        { id: 'usage', status: 'operational' },
      ],
    },
  );
});

test('rejects malformed or unhealthy responses', () => {
  assert.deepEqual(normalizePublicStatus({ status: 'unknown', checkedAt: 'not-a-date' }), {
    status: 'outage', checkedAt: '', components: [
      { id: 'gateway', status: 'outage' },
      { id: 'billing', status: 'outage' },
      { id: 'usage', status: 'outage' },
    ],
  });
  assert.deepEqual(normalizePublicStatus(null), {
    status: 'outage',
    checkedAt: '',
    components: [
      { id: 'gateway', status: 'outage' },
      { id: 'billing', status: 'outage' },
      { id: 'usage', status: 'outage' },
    ],
  });
});

test('fails closed when operational evidence is incomplete', () => {
  const status = normalizePublicStatus({
    status: 'operational',
    checkedAt: '2026-07-28T15:00:00Z',
    components: [{ id: 'gateway', status: 'operational' }],
  });
  assert.equal(status.status, 'outage');
  assert.equal(status.components.find((component) => component.id === 'billing')?.status, 'outage');
});
