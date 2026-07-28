import assert from 'node:assert/strict';
import test from 'node:test';

import { googleAuthPath, googleErrorKey } from './google-auth.ts';

test('starts Google OAuth through the same-origin BFF with a safe locale', () => {
  assert.equal(googleAuthPath('vi'), '/api/v1/auth/google/start?locale=vi');
  assert.equal(googleAuthPath('en'), '/api/v1/auth/google/start?locale=en');
  assert.equal(googleAuthPath('unexpected'), '/api/v1/auth/google/start?locale=vi');
});

test('maps known callback failures to their own message key', () => {
  assert.equal(googleErrorKey('cancelled'), 'cancelled');
  assert.equal(googleErrorKey('account_conflict'), 'account_conflict');
  assert.equal(googleErrorKey('suspended'), 'suspended');
});

test('never renders an attacker-chosen code from the query string', () => {
  assert.equal(googleErrorKey('<script>alert(1)</script>'), 'generic');
  assert.equal(googleErrorKey('internal'), 'generic');
  assert.equal(googleErrorKey(null), null);
  assert.equal(googleErrorKey(''), null);
});
