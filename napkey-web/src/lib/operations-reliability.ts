import type { OperationsSummary } from './api/types';

export function normalizeOperationsReliability(data: OperationsSummary): OperationsSummary['reliability'] {
  const value = (data as OperationsSummary & { reliability?: unknown }).reliability;
  if (value && typeof value === 'object') {
    const raw = value as Record<string, unknown>;
    if (Array.isArray(raw.issues) && isFiniteNumber(raw.errorRatePercent) && isFiniteNumber(raw.availablePercent)) {
      let issues = raw.issues.flatMap((item) => {
        if (!item || typeof item !== 'object') return [];
        const issue = item as Record<string, unknown>;
        if (typeof issue.code !== 'string' || (issue.severity !== 'degraded' && issue.severity !== 'outage')) return [];
        const severity: 'degraded' | 'outage' = issue.severity;
        return [{ code: issue.code, severity }];
      });
      const status = raw.status === 'operational' || raw.status === 'degraded' || raw.status === 'outage' ? raw.status : 'outage';
      if (status !== 'operational' && issues.length === 0) {
        issues = [{ code: 'unknown_reliability_issue', severity: status }];
      }
      return {
        status,
        issues,
        errorRatePercent: Math.min(100, Math.max(0, raw.errorRatePercent as number)),
        availablePercent: Math.min(100, Math.max(0, raw.availablePercent as number)),
      };
    }
  }
  const accounts = data.dataPlane.accounts ?? 0;
  const available = data.dataPlane.available ?? 0;
  return {
    status: data.dataPlane.healthy ? 'operational' : 'outage',
    issues: data.dataPlane.healthy ? [] : [{ code: 'data_plane_unreachable', severity: 'outage' }],
    errorRatePercent: 0,
    availablePercent: accounts > 0 ? Math.round((available / accounts) * 1000) / 10 : 0,
  };
}

function isFiniteNumber(value: unknown): value is number {
  return typeof value === 'number' && Number.isFinite(value);
}
