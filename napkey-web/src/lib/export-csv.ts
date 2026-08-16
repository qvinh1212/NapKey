import type { UsageRecord } from './api/types';

export function exportUsageCsv(records: readonly UsageRecord[], filename = 'napkey-usage-ledger.csv') {
  if (!records.length) return;

  const headers = [
    'Request ID',
    'Date & Time (UTC)',
    'Model',
    'Status',
    'Prompt Tokens',
    'Completion Tokens',
    'Cache Read Tokens',
    'Total Tokens',
    'Total Cost (VND)',
    'Latency (ms)',
    'API Key Name',
    'API Key ID',
  ];

  const rows = records.map((record) => {
    return [
      `"${record.requestId || record.id}"`,
      `"${record.createdAt}"`,
      `"${record.model}"`,
      `"${record.status}"`,
      record.tokens?.input ?? 0,
      record.tokens?.output ?? 0,
      record.tokens?.cacheRead ?? 0,
      record.tokens?.total ?? 0,
      record.cost ?? 0,
      record.latencyMs ?? 0,
      `"${record.keyName || ''}"`,
      `"${record.keyId || ''}"`,
    ].join(',');
  });

  const csvContent = '\uFEFF' + [headers.join(','), ...rows].join('\r\n');
  const blob = new Blob([csvContent], { type: 'text/csv;charset=utf-8;' });
  const url = URL.createObjectURL(blob);
  const link = document.createElement('a');
  link.setAttribute('href', url);
  link.setAttribute('download', filename);
  document.body.appendChild(link);
  link.click();
  document.body.removeChild(link);
  URL.revokeObjectURL(url);
}
