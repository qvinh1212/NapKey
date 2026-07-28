import assert from 'node:assert/strict';
import test from 'node:test';
import { normalizeModelCatalog } from './model-catalog.ts';

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
  const expected = {
    live: false,
    models: [
      { id: 'auto', family: 'auto', thinking: false },
      { id: 'claude-sonnet-4-6', family: 'sonnet', thinking: false },
      { id: 'claude-sonnet-4-6-thinking', family: 'sonnet', thinking: true },
    ],
  };

  assert.deepEqual(normalizeModelCatalog(null), expected);
  assert.deepEqual(normalizeModelCatalog({ object: 'list', data: [] }), expected);
});
