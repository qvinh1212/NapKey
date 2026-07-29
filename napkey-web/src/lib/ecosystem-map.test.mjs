import assert from 'node:assert/strict';
import test from 'node:test';
import {
  ecosystemClients,
  ecosystemInboundSignals,
  ecosystemModels,
  ecosystemOutboundSignals,
} from './ecosystem-map.ts';

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

test('defines staggered signal routes through the NapKey hub', () => {
  assert.equal(ecosystemInboundSignals.length, 6);
  assert.equal(ecosystemOutboundSignals.length, 4);

  for (const signal of ecosystemInboundSignals) {
    assert.match(signal.path, /^M 350 /);
    assert.match(signal.path, /600 310$/);
  }

  for (const signal of ecosystemOutboundSignals) {
    assert.match(signal.path, /^M 600 310 /);
    assert.match(signal.path, /850 \d+$/);
  }

  assert.equal(new Set(ecosystemInboundSignals.map(({ delay }) => delay)).size, 6);
  assert.equal(new Set(ecosystemOutboundSignals.map(({ delay }) => delay)).size, 4);
  assert.ok([...ecosystemInboundSignals, ...ecosystemOutboundSignals].every(({ duration }) => duration >= 2.4));
});
