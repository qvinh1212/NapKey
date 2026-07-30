import assert from 'node:assert/strict';
import test from 'node:test';

import { activationState } from './activation.ts';

test('prompts a verified user with no API key to create their first key', () => {
  assert.deepEqual(
    activationState({ activeKeys: 0, totalRequests: 0 }),
    { stage: 'create_key', completedSteps: 1 },
  );
});

test('prompts a user with a key but no request to connect and test it', () => {
  assert.deepEqual(
    activationState({ activeKeys: 1, totalRequests: 0 }),
    { stage: 'send_request', completedSteps: 2 },
  );
});

test('hides activation once the first request is recorded', () => {
  assert.deepEqual(
    activationState({ activeKeys: 1, totalRequests: 1 }),
    { stage: 'activated', completedSteps: 3 },
  );
});

