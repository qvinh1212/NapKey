import assert from 'node:assert/strict';
import test from 'node:test';
import { ecosystemClients, ecosystemModels } from './ecosystem-map.ts';

test('maps NapKey to supported developer clients and Claude model families', () => {
  assert.deepEqual(
    ecosystemClients.map((client) => client.id),
    ['claudeCode', 'cursor', 'vscode', 'cline', 'openCode', 'sdk'],
  );
  assert.deepEqual(
    ecosystemModels.map((model) => model.id),
    ['claude-opus-4.8', 'claude-sonnet-4.6', 'claude-haiku-4.5', 'auto'],
  );
});

test('does not advertise unrelated model providers', () => {
  const advertised = ecosystemModels.map((model) => model.id).join(' ').toLowerCase();
  for (const provider of ['gemini', 'deepseek', 'grok', 'qwen', 'llama', 'glm']) {
    assert.doesNotMatch(advertised, new RegExp(provider));
  }
});
