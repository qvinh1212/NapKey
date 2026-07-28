import assert from 'node:assert/strict';
import test from 'node:test';
import { normalizeOperationsReliability } from './operations-reliability.ts';

test('falls back safely during a rolling deployment with an older backend', () => {
  const value = normalizeOperationsReliability({
    dataPlane: { healthy: false, accounts: 4, available: 1 },
  });
  assert.deepEqual(value, {
    status: 'outage',
    issues: [{ code: 'data_plane_unreachable', severity: 'outage' }],
    errorRatePercent: 0,
    availablePercent: 25,
  });
});

test('filters malformed reliability issues', () => {
  const value = normalizeOperationsReliability({
    reliability: {
      status: 'degraded', errorRatePercent: 12, availablePercent: 25,
      issues: [{ code: 'error_rate_high', severity: 'degraded' }, { code: 7, severity: 'outage' }],
    },
    dataPlane: { healthy: true },
  });
  assert.deepEqual(value.issues, [{ code: 'error_rate_high', severity: 'degraded' }]);
});

test('fails closed when non-operational evidence is malformed', () => {
  const value = normalizeOperationsReliability({
    reliability: {
      status: 'outage', errorRatePercent: 150, availablePercent: -5,
      issues: [{ code: 7, severity: 'outage' }],
    },
    dataPlane: { healthy: true },
  });
  assert.deepEqual(value, {
    status: 'outage',
    issues: [{ code: 'unknown_reliability_issue', severity: 'outage' }],
    errorRatePercent: 100,
    availablePercent: 0,
  });
});
