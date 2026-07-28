import assert from 'node:assert/strict';
import test from 'node:test';
import { trustedClientIP } from './proxy-client-ip.ts';

test('selects the rightmost valid address appended by the edge proxy', () => {
  assert.equal(trustedClientIP('198.51.100.1, 203.0.113.9', null), '203.0.113.9');
});

test('ignores malformed forwarded values and falls back to a valid real IP', () => {
  assert.equal(trustedClientIP('spoofed, not-an-ip', '203.0.113.10'), '203.0.113.10');
});

test('does not forward an unusable address', () => {
  assert.equal(trustedClientIP('spoofed', 'also-invalid'), null);
});
