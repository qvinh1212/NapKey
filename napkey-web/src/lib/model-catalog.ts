export type PublicModel = {
  id: string;
  family: 'auto' | 'opus' | 'sonnet' | 'haiku' | 'openai-alias' | 'other';
  thinking: boolean;
};

export type ModelCatalog = {
  live: boolean;
  models: PublicModel[];
};

const fallbackModels = ['auto', 'claude-sonnet-4-6', 'claude-sonnet-4-6-thinking'];

function modelFamily(id: string): PublicModel['family'] {
  const normalized = id.toLowerCase();
  if (normalized === 'auto') return 'auto';
  if (normalized.includes('opus')) return 'opus';
  if (normalized.includes('sonnet')) return 'sonnet';
  if (normalized.includes('haiku')) return 'haiku';
  if (normalized.startsWith('gpt-')) return 'openai-alias';
  return 'other';
}

function publicModel(id: string): PublicModel {
  return {
    id,
    family: modelFamily(id),
    thinking: id.toLowerCase().endsWith('-thinking'),
  };
}

function fallbackCatalog(): ModelCatalog {
  return { live: false, models: fallbackModels.map(publicModel) };
}

export function normalizeModelCatalog(value: unknown): ModelCatalog {
  if (value === null || typeof value !== 'object') return fallbackCatalog();
  const data = (value as Record<string, unknown>).data;
  if (!Array.isArray(data)) return fallbackCatalog();

  const ids = [...new Set(data.flatMap((entry) => {
    if (entry === null || typeof entry !== 'object') return [];
    const id = (entry as Record<string, unknown>).id;
    return typeof id === 'string' && id.trim() ? [id.trim()] : [];
  }))].sort((left, right) => left.localeCompare(right));

  if (ids.length === 0) return fallbackCatalog();
  return { live: true, models: ids.map(publicModel) };
}

export async function readModelCatalog(): Promise<ModelCatalog> {
  const { site } = await import('./site');
  try {
    const response = await fetch(`${site.apiBaseUrl.replace(/\/+$/, '')}/v1/models`, {
      next: { revalidate: 60 },
      signal: AbortSignal.timeout(2500),
    });
    if (!response.ok) return fallbackCatalog();
    return normalizeModelCatalog(await response.json());
  } catch {
    return fallbackCatalog();
  }
}
