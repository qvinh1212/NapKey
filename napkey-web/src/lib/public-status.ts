export type PublicStatus = {
  operational: boolean;
  version: string;
  uptimeSeconds: number;
};

export function normalizePublicStatus(value: unknown): PublicStatus {
  const body = value !== null && typeof value === 'object' ? value as Record<string, unknown> : {};
  const uptime = typeof body.uptime === 'number' && body.uptime >= 0 ? body.uptime : 0;

  return {
    operational: body.status === 'ok',
    version: typeof body.version === 'string' ? body.version : '',
    uptimeSeconds: uptime,
  };
}

export async function readPublicStatus(): Promise<PublicStatus> {
  const { site } = await import('./site');
  try {
    const response = await fetch(`${site.apiBaseUrl.replace(/\/+$/, '')}/health`, {
      cache: 'no-store',
      signal: AbortSignal.timeout(2000),
    });
    if (!response.ok) return normalizePublicStatus(null);
    return normalizePublicStatus(await response.json());
  } catch {
    return normalizePublicStatus(null);
  }
}

export function compactUptime(seconds: number) {
  const days = Math.floor(seconds / 86400);
  const hours = Math.floor((seconds % 86400) / 3600);
  const minutes = Math.floor((seconds % 3600) / 60);
  if (days > 0) return `${days}d ${hours}h`;
  if (hours > 0) return `${hours}h ${minutes}m`;
  return `${minutes}m`;
}
