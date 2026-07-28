import assert from 'node:assert/strict';
import test from 'node:test';
import { isValidOnboardingResponse, onboardingSnippet, parseOnboardingResponse } from './onboarding.ts';

const secret = 'nk_live_test_secret';
const base = 'https://api.example.com/';

test('builds Claude Code environment with the newly created key', () => {
  const snippet = onboardingSnippet('claudeCode', secret, base);

  assert.equal(snippet.lang, 'bash');
  assert.match(snippet.code, /ANTHROPIC_BASE_URL="https:\/\/api\.example\.com"/);
  assert.match(snippet.code, /ANTHROPIC_AUTH_TOKEN="nk_live_test_secret"/);
  assert.doesNotMatch(snippet.code, /nk_live_\.\.\./);
});

test('builds SDK and curl snippets against the normalized API URL', () => {
  assert.match(onboardingSnippet('anthropic', secret, base).code, /base_url="https:\/\/api\.example\.com"/);
  assert.match(onboardingSnippet('openai', secret, base).code, /baseURL: 'https:\/\/api\.example\.com\/v1'/);
  assert.match(onboardingSnippet('curl', secret, base).code, /https:\/\/api\.example\.com\/v1\/messages/);
});

test('extracts readable text and usage from an Anthropic response', () => {
  assert.deepEqual(
    parseOnboardingResponse({
      id: 'msg_123',
      model: 'claude-sonnet-4.6',
      content: [{ type: 'text', text: 'NapKey ready' }],
      usage: { input_tokens: 12, output_tokens: 3 },
    }),
    {
      id: 'msg_123',
      model: 'claude-sonnet-4.6',
      text: 'NapKey ready',
      inputTokens: 12,
      outputTokens: 3,
    },
  );
});

test('handles an incomplete upstream response safely', () => {
  const result = parseOnboardingResponse(null);
  assert.deepEqual(result, {
    id: '',
    model: '',
    text: '',
    inputTokens: 0,
    outputTokens: 0,
  });
  assert.equal(isValidOnboardingResponse(result), false);
});

test('accepts only a usable Anthropic message response', () => {
  assert.equal(isValidOnboardingResponse({ id: 'msg_1', model: 'claude', text: 'NapKey ready', inputTokens: 8, outputTokens: 2 }), true);
  assert.equal(isValidOnboardingResponse({ id: '', model: '', text: '', inputTokens: 0, outputTokens: 0 }), false);
});
