import assert from 'node:assert/strict';
import test from 'node:test';
import { defaultModel, normalizeModelCatalog } from './model-catalog.ts';

test('normalizes, deduplicates, and sorts a public OpenAI-style model catalog', () => {
  assert.deepEqual(normalizeModelCatalog({
    object: 'list',
    data: [
      { id: 'claude-sonnet-4-6-thinking', object: 'model', owned_by: 'anthropic', internal: 'secret' },
      { id: 'claude-sonnet-4-6', object: 'model', owned_by: 'anthropic' },
      { id: 'claude-sonnet-4-6', object: 'model', owned_by: 'duplicate' },
      { id: 'auto', object: 'model', owned_by: 'napkey' },
    ],
  }), {
    live: true,
    models: [
      { id: 'auto', family: 'auto', thinking: false },
      { id: 'claude-sonnet-4-6', family: 'sonnet', thinking: false },
      { id: 'claude-sonnet-4-6-thinking', family: 'sonnet', thinking: true },
    ],
  });
});

test('ignores malformed entries and exposes only safe catalog fields', () => {
  assert.deepEqual(normalizeModelCatalog({
    object: 'list',
    data: [null, {}, { id: '' }, { id: 42 }, { id: 'gpt-4o', upstream_token: 'private' }],
  }), {
    live: true,
    models: [{ id: 'gpt-4o', family: 'openai-alias', thinking: false }],
  });
});

test('returns a stable fallback when the payload is malformed or empty', () => {
  // Derived from defaultModel rather than repeated, so the fallback cannot drift away
  // from the model every snippet tells a customer to send.
  const expected = {
    live: false,
    models: [
      { id: 'auto', family: 'auto', thinking: false },
      { id: defaultModel, family: 'sonnet', thinking: false },
    ],
  };

  assert.deepEqual(normalizeModelCatalog(null), expected);
  assert.deepEqual(normalizeModelCatalog({ object: 'list', data: [] }), expected);
});

// The documented model must be one the price book covers. A snippet that names an
// unpriced model bills through the '*' fallback rate instead of its own row, and the
// first request a new customer sends is the worst place to discover that.
test('the default model is one of the priced models on sale', () => {
  assert.ok(
    ['claude-sonnet-5', 'claude-opus-5', 'claude-opus-4.7', 'claude-opus-4.8'].includes(defaultModel),
    `${defaultModel} has no row in the seeded price book`,
  );
});
