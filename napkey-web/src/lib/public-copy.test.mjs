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

// Billing copy is a promise about what the customer is charged.
//
// The token path carries a flat per-request fee (migration 0019), so copy claiming
// there is no per-request minimum would be a promise the invoice breaks. Asserted per
// locale because a translation is just as binding as the English.
for (const locale of ['vi', 'en']) {
  test(`${locale} billing copy does not deny the per-request fee`, () => {
    const messages = readFileSync(new URL(`../../messages/${locale}.json`, import.meta.url), 'utf8');
    for (const denial of [
      /no per-request minimum/i,
      /kh?ng ph? t?i thi?u m?i request/i,
      /no request fee/i,
      /ch? tr? theo token/i,
    ]) {
      assert.doesNotMatch(messages, denial);
    }
  });
}

test('hardcoded customer UI copy does not expose the upstream implementation', () => {
  const dashboard = readFileSync(
    new URL('../components/console/operations-dashboard.tsx', import.meta.url),
    'utf8',
  );
  assert.doesNotMatch(dashboard, /kiro(?:-go| go)?/i);
});
