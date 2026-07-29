type UsageRecordShape = {
  status: 'success' | 'error' | 'cancelled';
  estimated: boolean;
  unpriced: boolean;
  tokens: { cacheRead: number; cacheWrite: number };
};

export function usageRecordView(record: UsageRecordShape) {
  const statusTone: 'accent' | 'danger' | 'neutral' =
    record.status === 'success' ? 'accent' : record.status === 'error' ? 'danger' : 'neutral';
  return {
    type: 'chat' as const,
    status: record.status,
    statusTone,
    cacheTokens: record.tokens.cacheRead + record.tokens.cacheWrite,
    quality: record.unpriced ? ('unpriced' as const) : record.estimated ? ('estimated' as const) : null,
  };
}
