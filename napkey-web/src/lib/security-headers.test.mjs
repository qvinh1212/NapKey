import assert from 'node:assert/strict';
import test from 'node:test';
import { webSecurityHeaders } from './security-headers.ts';

test('locks the web surface to known content and API origins', () => {
  const headers = Object.fromEntries(webSecurityHeaders('https://api.napkey.io.vn/v1').map(({ key, value }) => [key, value]));
  assert.match(headers['Content-Security-Policy'], /default-src 'self'/);
  assert.match(headers['Content-Security-Policy'], /connect-src 'self' https:\/\/api\.napkey\.io\.vn/);
  assert.match(headers['Content-Security-Policy'], /object-src 'none'/);
  assert.match(headers['Content-Security-Policy'], /frame-ancestors 'none'/);
  assert.equal(headers['Permissions-Policy'].includes('camera=()'), true);
  assert.equal(headers['Strict-Transport-Security'].includes('includeSubDomains'), true);
});

test('does not add an invalid API origin to CSP', () => {
  const headers = Object.fromEntries(webSecurityHeaders('javascript:alert(1)').map(({ key, value }) => [key, value]));
  assert.doesNotMatch(headers['Content-Security-Policy'], /javascript:/);
});
