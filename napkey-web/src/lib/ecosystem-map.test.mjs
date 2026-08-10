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
    ['claude-opus-5', 'claude-sonnet-5', 'claude-haiku-4.5', 'auto'],
  );
});

// The landing diagram is a promise about what the API accepts. Naming a model with no
// price row would advertise something that bills through the '*' fallback rate, so the
// marketing surface is pinned to the models actually on sale.
test('only advertises models the price book covers', () => {
  const priced = new Set([
    'claude-sonnet-5',
    'claude-opus-5',
    'claude-opus-4.7',
    'claude-opus-4.8',
    'claude-haiku-4.5',
    // Resolved by the gateway before anything reaches the upstream.
    'auto',
  ]);
  for (const { id } of ecosystemModels) {
    assert.ok(priced.has(id), `${id} is advertised but has no seeded price row`);
  }
});

// Each row renders its family as an i18n label, so a duplicated family shows the same
// caption twice and reads like a rendering bug.
test('each advertised model has a distinct family label', () => {
  const families = ecosystemModels.map((model) => model.family);
  assert.equal(new Set(families).size, families.length);
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
