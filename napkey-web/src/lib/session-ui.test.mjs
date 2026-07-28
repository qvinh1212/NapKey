import assert from 'node:assert/strict';
import test from 'node:test';
import { publicAuthAction, shouldRedirectFromSignIn } from './session-ui.ts';

test('authenticated visitors see the dashboard action', () => {
  assert.deepEqual(publicAuthAction('authenticated'), {
    href: '/console',
    labelKey: 'dashboard',
  });
});

test('anonymous visitors keep the sign-in action', () => {
  assert.deepEqual(publicAuthAction('anonymous'), {
    href: '/signin',
    labelKey: 'signIn',
  });
});

test('only authenticated visitors are redirected away from sign-in', () => {
  assert.equal(shouldRedirectFromSignIn('authenticated'), true);
  assert.equal(shouldRedirectFromSignIn('anonymous'), false);
  assert.equal(shouldRedirectFromSignIn('loading'), false);
});
