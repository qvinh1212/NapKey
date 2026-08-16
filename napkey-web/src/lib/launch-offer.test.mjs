import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import test from 'node:test';

const sourcePath = new URL('../components/napkey/launch-offer.tsx', import.meta.url);

test('launch offer preserves dismissal and session-aware cta', async () => {
  const source = await readFile(sourcePath, 'utf8');
  for (const contract of [
    'DISMISS_DAYS = 7',
    "event.key === 'Escape'",
    'rememberDismissal',
    "session.status === 'loading'",
    'role="region"',
  ]) {
    assert.match(source, new RegExp(contract.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')));
  }
});
