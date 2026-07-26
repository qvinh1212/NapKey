type UsagePageQuery = {
  from: string;
  to: string;
  keyId?: string;
  limit: number;
  offset: number;
};

function query(params: Record<string, string | number | undefined>): string {
  const search = new URLSearchParams();
  for (const [key, value] of Object.entries(params)) {
    if (value !== undefined && value !== '') search.set(key, String(value));
  }
  const value = search.toString();
  return value ? `?${value}` : '';
}

/** Keep the chart and ledger on one date window while pagination only affects rows. */
export function usagePageQueries(params: UsagePageQuery) {
  const range = { from: params.from, to: params.to, keyId: params.keyId };

  return {
    detail: `/v1/me/usage/detail${query(range)}`,
    records: `/v1/me/usage/records${query(params)}`,
  };
}
