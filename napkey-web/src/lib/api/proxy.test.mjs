import assert from 'node:assert/strict';
import test from 'node:test';
import { isProxyPathAllowed } from './proxy-policy.ts';

test('allows the PayOS webhook to reach the control plane', () => {
  assert.equal(isProxyPathAllowed('/webhooks/payos'), true);
});

test('allows the public reliability status endpoint', () => {
  assert.equal(isProxyPathAllowed('/v1/status'), true);
});

test('keeps internal control-plane endpoints private', () => {
  assert.equal(isProxyPathAllowed('/internal/usage'), false);
  assert.equal(isProxyPathAllowed('/webhooks/casso'), false);
});

test('allows the permission-protected business summary only by exact path', () => {
  assert.equal(isProxyPathAllowed('/v1/admin/business/summary'), true);
  assert.equal(isProxyPathAllowed('/v1/admin/business/summary/export'), false);
});
