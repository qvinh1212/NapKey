import assert from 'node:assert/strict';
import test from 'node:test';
import { usagePageQueries } from './usage-query.ts';

test('builds detail and ledger URLs from the same date range', () => {
  assert.deepEqual(
    usagePageQueries({
      from: '2026-07-01',
      to: '2026-08-01',
      keyId: 'key-123',
      limit: 50,
      offset: 100,
    }),
    {
      detail: '/v1/me/usage/detail?from=2026-07-01&to=2026-08-01&keyId=key-123',
      records:
        '/v1/me/usage/records?from=2026-07-01&to=2026-08-01&keyId=key-123&limit=50&offset=100',
    },
  );
});

test('omits an empty key filter', () => {
  const queries = usagePageQueries({
    from: '2026-07-01',
    to: '2026-08-01',
    keyId: '',
    limit: 50,
    offset: 0,
  });

  assert.equal(queries.records.includes('keyId='), false);
});
