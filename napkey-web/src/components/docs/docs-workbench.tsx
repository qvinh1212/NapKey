'use client';

import { useMemo, useState } from 'react';
import { useTranslations } from 'next-intl';
import { developerSnippet, normalizeDeveloperModel, type DeveloperTool } from '@/lib/developer-tools';
import type { PublicModel } from '@/lib/model-catalog';

const tools: DeveloperTool[] = ['claudeCode', 'anthropic', 'openai', 'curl', 'powershell'];

export function DocsWorkbench({ models, apiBaseUrl }: { models: PublicModel[]; apiBaseUrl: string }) {
  const t = useTranslations('docsPage.quickstart');
  const [tool, setTool] = useState<DeveloperTool>('claudeCode');
  const [model, setModel] = useState(() => normalizeDeveloperModel('auto', models));
  const [copied, setCopied] = useState(false);
  const snippet = useMemo(() => developerSnippet(tool, model, apiBaseUrl), [apiBaseUrl, model, tool]);

  async function copySnippet() {
    try {
      await navigator.clipboard.writeText(snippet.code);
      setCopied(true);
      window.setTimeout(() => setCopied(false), 1800);
    } catch {
      setCopied(false);
    }
  }

  return (
    <div className="overflow-hidden rounded-lg border border-line bg-surface">
      <div className="grid gap-px bg-line lg:grid-cols-[0.72fr_1.28fr]">
        <div className="bg-surface p-5 sm:p-6">
          <label htmlFor="docs-model" className="font-mono text-label tracking-[0.12em] text-dim uppercase">
            {t('modelLabel')}
          </label>
          <select
            id="docs-model"
            value={model}
            onChange={(event) => setModel(event.target.value)}
            className="mt-3 w-full rounded-md border border-line bg-black px-4 py-3 font-mono text-ui text-fg outline-none focus:border-accent"
          >
            {models.map((item) => <option key={item.id} value={item.id}>{item.id}</option>)}
          </select>

          <p className="mt-6 font-mono text-label tracking-[0.12em] text-dim uppercase">{t('toolLabel')}</p>
          <div role="group" aria-label={t('toolLabel')} className="mt-3 flex flex-wrap gap-2">
            {tools.map((item) => (
              <button
                key={item}
                type="button"
                aria-pressed={tool === item}
                onClick={() => { setTool(item); setCopied(false); }}
                className={`rounded-full border px-3 py-1.5 text-ui transition-colors ${tool === item ? 'border-accent/50 bg-accent-soft text-accent-light' : 'border-line text-muted hover:text-fg'}`}
              >
                {t(`tools.${item}`)}
              </button>
            ))}
          </div>

          <div className="mt-6 rounded-md border border-info/30 bg-info/10 p-4">
            <p className="text-ui font-medium text-info">{t('secretTitle')}</p>
            <p className="mt-2 text-ui leading-relaxed text-muted">{t('secretBody')}</p>
            <code className="mt-3 block font-mono text-xs text-fg">NAPKEY_API_KEY</code>
          </div>
        </div>

        <div className="min-w-0 bg-black/35 p-5 sm:p-6">
          <div className="flex flex-wrap items-center justify-between gap-3">
            <div>
              <p className="font-mono text-label tracking-[0.12em] text-accent uppercase">{t('codeLabel')}</p>
              <p className="mt-1 text-ui text-dim">{t('codeHint')}</p>
            </div>
            <button type="button" onClick={() => void copySnippet()} className="rounded-full border border-line px-4 py-2 text-ui text-muted hover:bg-white/5 hover:text-fg">
              {copied ? t('copied') : t('copy')}
            </button>
          </div>
          <pre className="mt-4 max-h-[32rem] max-w-full overflow-auto rounded-md border border-line bg-black p-4 font-mono text-xs leading-relaxed text-zinc-300 sm:p-5">
            <code>{snippet.code}</code>
          </pre>
          <p aria-live="polite" className="sr-only">{copied ? t('copied') : ''}</p>
        </div>
      </div>
    </div>
  );
}

