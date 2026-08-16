'use client';

import { useMemo, useState } from 'react';
import { useTranslations } from 'next-intl';
import { Link } from '@/i18n/navigation';
import { developerSnippet, diagnoseApiFailure, normalizeDeveloperModel, type DeveloperTool } from '@/lib/developer-tools';
import type { ModelCatalog } from '@/lib/model-catalog';
import { Badge, Panel, PanelHeader } from './ui';
import { CopyButton } from '@/components/ui/copy-button';

const tools: DeveloperTool[] = ['claudeCode', 'cursor', 'cline', 'windsurf', 'langchain', 'anthropic', 'openai', 'curl', 'powershell'];
const failureStatuses = [400, 401, 402, 429, 503] as const;

export function DeveloperWorkbench({ catalog, apiBaseUrl }: { catalog: ModelCatalog; apiBaseUrl: string }) {
  const t = useTranslations('console.developer');
  const [tool, setTool] = useState<DeveloperTool>('claudeCode');
  const [model, setModel] = useState(() => normalizeDeveloperModel('auto', catalog.models));
  const snippet = useMemo(() => developerSnippet(tool, model, apiBaseUrl), [apiBaseUrl, model, tool]);

  return (
    <div className="flex flex-col gap-6">
      <Panel as="section" className="overflow-hidden">
        <PanelHeader
          title={t('workbenchTitle')}
          description={t('workbenchDescription')}
          action={<Badge tone={catalog.live ? 'accent' : 'warn'}>{catalog.live ? t('catalogLive') : t('catalogFallback')}</Badge>}
        />
        <div className="grid gap-px bg-line lg:grid-cols-[0.72fr_1.28fr]">
          <div className="bg-surface p-5">
            <label htmlFor="developer-model" className="font-mono text-label tracking-[0.12em] text-dim uppercase">{t('modelLabel')}</label>
            <select id="developer-model" value={model} onChange={(event) => setModel(event.target.value)} className="mt-3 w-full rounded-[10px] border border-line bg-terminal px-3.5 py-2.5 font-mono text-ui text-fg outline-none focus:border-accent">
              {catalog.models.map((item) => <option key={item.id} value={item.id}>{item.id}</option>)}
            </select>
            <p className="mt-3 text-ui leading-relaxed text-dim">{t('modelHint')}</p>

            <p className="mt-6 font-mono text-label tracking-[0.12em] text-dim uppercase">{t('clientLabel')}</p>
            <div role="group" aria-label={t('clientLabel')} className="mt-3 flex flex-wrap gap-2">
              {tools.map((item) => (
                <button key={item} type="button" aria-pressed={tool === item} onClick={() => setTool(item)} className={`rounded-full border px-3 py-1.5 text-ui transition-colors ${tool === item ? 'border-accent/50 bg-accent-soft text-accent-light' : 'border-line text-muted hover:bg-white/5'}`}>
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

          <div className="min-w-0 bg-surface p-5">
            <div className="flex flex-wrap items-center justify-between gap-3">
              <div><p className="font-mono text-label tracking-[0.12em] text-dim uppercase">{t('snippetLabel')}</p><p className="mt-1 text-ui text-dim">{t('snippetHint')}</p></div>
              <CopyButton
                value={snippet.code}
                label={t('copy')}
                copiedLabel={t('copied')}
                variant="pill"
                showTooltip
              />
            </div>
            <pre className="mt-4 max-h-[30rem] overflow-auto rounded-lg border border-line bg-terminal p-4 font-mono text-xs leading-relaxed text-muted"><code>{snippet.code}</code></pre>
            <div className="mt-4 flex flex-wrap items-center justify-between gap-3 rounded-[10px] border border-line bg-terminal px-4 py-3">
              <code className="min-w-0 truncate font-mono text-ui text-accent-light">{apiBaseUrl}</code>
              <CopyButton
                value={apiBaseUrl}
                label={t('copyEndpoint')}
                copiedLabel={t('copied')}
                variant="ghost"
                showTooltip
                className="shrink-0"
              />
            </div>
          </div>
        </div>
      </Panel>

      <Panel as="section">
        <PanelHeader
          title={t('controlsTitle')}
          description={t('controlsDescription')}
          action={<Badge tone="accent">{t('controlsBadge')}</Badge>}
        />
        <div className="grid gap-px bg-line sm:grid-cols-2 lg:grid-cols-4">
          <div className="bg-surface p-5">
            <span className="font-mono text-micro text-accent-light uppercase tracking-wider">01 · Privacy</span>
            <h3 className="mt-2 text-sm font-medium text-fg">{t('controls.zeroLogging.title')}</h3>
            <p className="mt-1.5 text-ui text-dim leading-relaxed">{t('controls.zeroLogging.body')}</p>
          </div>
          <div className="bg-surface p-5">
            <span className="font-mono text-micro text-accent-light uppercase tracking-wider">02 · Guardrails</span>
            <h3 className="mt-2 text-sm font-medium text-fg">{t('controls.hardCaps.title')}</h3>
            <p className="mt-1.5 text-ui text-dim leading-relaxed">{t('controls.hardCaps.body')}</p>
          </div>
          <div className="bg-surface p-5">
            <span className="font-mono text-micro text-accent-light uppercase tracking-wider">03 · Network</span>
            <h3 className="mt-2 text-sm font-medium text-fg">{t('controls.smartRouting.title')}</h3>
            <p className="mt-1.5 text-ui text-dim leading-relaxed">{t('controls.smartRouting.body')}</p>
          </div>
          <div className="bg-surface p-5">
            <span className="font-mono text-micro text-accent-light uppercase tracking-wider">04 · Isolation</span>
            <h3 className="mt-2 text-sm font-medium text-fg">{t('controls.keyScoping.title')}</h3>
            <p className="mt-1.5 text-ui text-dim leading-relaxed">{t('controls.keyScoping.body')}</p>
          </div>
        </div>
      </Panel>

      <Panel as="section">
        <PanelHeader title={t('errorsTitle')} description={t('errorsDescription')} />
        <div className="grid gap-px bg-line sm:grid-cols-2 xl:grid-cols-5">
          {failureStatuses.map((status) => {
            const diagnosis = diagnoseApiFailure(status);
            return <article key={status} className="bg-surface p-5"><div className="flex items-center justify-between gap-2"><code className="font-mono text-lg text-fg">HTTP {status}</code><Badge tone={diagnosis.retryable ? 'info' : 'neutral'}>{diagnosis.retryable ? t('retryable') : t('actionRequired')}</Badge></div><h3 className="mt-4 text-sm font-medium text-fg">{t(`errors.${diagnosis.key}.title`)}</h3><p className="mt-2 text-ui leading-relaxed text-dim">{t(`errors.${diagnosis.key}.body`)}</p></article>;
          })}
        </div>
      </Panel>

      <Panel as="section" className="p-5">
        <p className="font-mono text-label tracking-[0.12em] text-dim uppercase">{t('nextTitle')}</p>
        <div className="mt-4 flex flex-wrap gap-3">
          <Link href="/console/keys" className="rounded-full bg-fg px-5 py-2 text-ui font-medium text-bg">{t('manageKeys')}</Link>
          <Link href="/console/usage" className="rounded-full border border-line px-5 py-2 text-ui text-muted hover:text-fg">{t('inspectUsage')}</Link>
          <Link href="/status" className="rounded-full border border-line px-5 py-2 text-ui text-muted hover:text-fg">{t('systemStatus')}</Link>
          <a href={`${apiBaseUrl}/v1/models`} target="_blank" rel="noreferrer" className="rounded-full border border-line px-5 py-2 text-ui text-muted hover:text-fg">GET /v1/models</a>
        </div>
      </Panel>
    </div>
  );
}
