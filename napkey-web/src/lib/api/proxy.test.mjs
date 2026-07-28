import assert from 'node:assert/strict';
import test from 'node:test';
import { isProxyPathAllowed } from './proxy-policy.ts';

test('allows the PayOS webhook to reach the control plane', () => {
  assert.equal(isProxyPathAllowed('/webhooks/payos'), true);
});

test('keeps internal control-plane endpoints private', () => {
  assert.equal(isProxyPathAllowed('/internal/usage'), false);
  assert.equal(isProxyPathAllowed('/webhooks/casso'), false);
});
