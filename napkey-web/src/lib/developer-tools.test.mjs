import assert from 'node:assert/strict';
import test from 'node:test';
import {
  developerSnippet,
  diagnoseApiFailure,
  normalizeDeveloperModel,
} from './developer-tools.ts';

test('builds copy-ready snippets with environment variables instead of embedded secrets', () => {
  for (const tool of ['claudeCode', 'cursor', 'cline', 'windsurf', 'langchain', 'anthropic', 'openai', 'curl', 'powershell']) {
    const snippet = developerSnippet(tool, 'claude-sonnet-4-6', 'https://api.napkey.io.vn/');
    assert.match(snippet.code, /claude-sonnet-4-6/);
    assert.doesNotMatch(snippet.code, /nk_live_/);
    assert.match(snippet.code, /(NAPKEY_API_KEY|ANTHROPIC_AUTH_TOKEN)/);
  }
});

test('uses native PowerShell JSON serialization to avoid shell quoting failures', () => {
  const snippet = developerSnippet('powershell', 'auto', 'https://api.napkey.io.vn');
  assert.match(snippet.code, /ConvertTo-Json/);
  assert.match(snippet.code, /Invoke-RestMethod/);
  assert.doesNotMatch(snippet.code, /-d '\{/);
});

test('normalizes model selection against the advertised catalog', () => {
  const models = [{ id: 'auto' }, { id: 'claude-sonnet-4-6' }];
  assert.equal(normalizeDeveloperModel('claude-sonnet-4-6', models), 'claude-sonnet-4-6');
  assert.equal(normalizeDeveloperModel('made-up-model', models), 'auto');
  assert.equal(normalizeDeveloperModel('', []), 'auto');
});

test('maps common HTTP failures to stable developer actions', () => {
  assert.equal(diagnoseApiFailure(401).key, 'authentication');
  assert.equal(diagnoseApiFailure(402).key, 'balance');
  assert.equal(diagnoseApiFailure(429).key, 'rateLimit');
  assert.equal(diagnoseApiFailure(503).retryable, true);
  assert.equal(diagnoseApiFailure(400).retryable, false);
});
