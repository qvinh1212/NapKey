export type PublicStatus = {
  status: 'operational' | 'degraded' | 'outage';
  checkedAt: string;
  components: Array<{ id: 'gateway' | 'billing' | 'usage'; status: 'operational' | 'degraded' | 'outage' }>;
};

export function normalizePublicStatus(value: unknown): PublicStatus {
  const body = value !== null && typeof value === 'object' ? value as Record<string, unknown> : {};
  const validStatus = (status: unknown): status is PublicStatus['status'] =>
    status === 'operational' || status === 'degraded' || status === 'outage';
  const parsedComponents = Array.isArray(body.components) ? body.components.flatMap((item) => {
    if (!item || typeof item !== 'object') return [];
    const component = item as Record<string, unknown>;
    if (!['gateway', 'billing', 'usage'].includes(String(component.id)) || !validStatus(component.status)) return [];
    return [{ id: component.id as PublicStatus['components'][number]['id'], status: component.status }];
  }) : [];
  const rank = { operational: 0, degraded: 1, outage: 2 } as const;
  const expectedIds = ['gateway', 'billing', 'usage'] as const;
  const components = expectedIds.map((id) => parsedComponents.find((item) => item.id === id) ?? { id, status: 'outage' as const });
  const declaredStatus = validStatus(body.status) ? body.status : 'outage';
  const status = components.reduce<PublicStatus['status']>(
    (worst, component) => rank[component.status] > rank[worst] ? component.status : worst,
    declaredStatus,
  );
  const checkedAt = typeof body.checkedAt === 'string' && Number.isFinite(Date.parse(body.checkedAt))
    ? body.checkedAt
    : '';
  return {
    status,
    checkedAt,
    components,
  };
}

export async function readPublicStatus(): Promise<PublicStatus> {
  const coreURL = (process.env.NAPKEY_CORE_URL ?? 'http://127.0.0.1:8081').replace(/\/+$/, '');
  try {
    const response = await fetch(`${coreURL}/v1/status`, {
      cache: 'no-store',
      signal: AbortSignal.timeout(2000),
    });
    if (!response.ok) return normalizePublicStatus(null);
    return normalizePublicStatus(await response.json());
  } catch {
    return normalizePublicStatus(null);
  }
}
