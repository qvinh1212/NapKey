import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';

for (const locale of ['vi', 'en']) {
  test(`${locale} customer copy does not expose the upstream implementation`, () => {
    const messages = readFileSync(new URL(`../../messages/${locale}.json`, import.meta.url), 'utf8');
    assert.doesNotMatch(messages, /kiro(?:-go| go)?/i);
  });

  test(`${locale} hero advertises the actual 10,000 VND minimum top-up`, () => {
    const messages = JSON.parse(
      readFileSync(new URL(`../../messages/${locale}.json`, import.meta.url), 'utf8'),
    );
    assert.match(messages.hero.metrics.topUp.value, /^10K\b/);
  });
}

test('hardcoded customer UI copy does not expose the upstream implementation', () => {
  const dashboard = readFileSync(
    new URL('../components/console/operations-dashboard.tsx', import.meta.url),
    'utf8',
  );
  assert.doesNotMatch(dashboard, /kiro(?:-go| go)?/i);
});
