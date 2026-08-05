'use client';

import { useEffect, useMemo, useState } from 'react';
import { useTranslations } from 'next-intl';
import { Link } from '@/i18n/navigation';
import { api } from '@/lib/api/client';
import type { CreateKeyResponse, KeyListResponse, KeySyncState, Money, UsageRecordsResponse } from '@/lib/api/types';
import { isValidOnboardingResponse, onboardingSnippet, parseOnboardingResponse, type OnboardingTool } from '@/lib/onboarding';
import { defaultModel } from '@/lib/model-catalog';
import { site } from '@/lib/site';
import { Badge, Panel, PanelHeader } from './ui';

type TestState =
  | { status: 'idle' }
  | { status: 'running' }
  | { status: 'success'; text: string; model: string; tokens: number; cost?: Money }
  | { status: 'error'; message: string };

const tools: OnboardingTool[] = ['claudeCode', 'anthropic', 'openai', 'curl'];

function wait(ms: number) {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

export function KeyOnboarding({ created, onDone }: { created: CreateKeyResponse; onDone(): void }) {
  const t = useTranslations('console.keys.onboarding');
  const [tool, setTool] = useState<OnboardingTool>('claudeCode');
  const [saved, setSaved] = useState(false);
  const [copied, setCopied] = useState<'key' | 'snippet' | null>(null);
  const [test, setTest] = useState<TestState>({ status: 'idle' });
  const [syncState, setSyncState] = useState<KeySyncState>(created.details.syncState);
  const snippet = useMemo(
    () => onboardingSnippet(tool, created.key, site.apiBaseUrl),
    [created.key, tool],
  );
  const synced = syncState === 'synced';

  useEffect(() => {
    if (syncState !== 'pending') return;
    const controller = new AbortController();
    const timer = window.setInterval(async () => {
      try {
        const response = await api.get<KeyListResponse>('/v1/keys', controller.signal);
        const current = response.keys.find((key) => key.id === created.details.id);
        if (current) setSyncState(current.syncState);
      } catch {
        // Polling is advisory; the normal test error remains the source of truth.
      }
    }, 1500);

    return () => {
      controller.abort();
      window.clearInterval(timer);
    };
  }, [created.details.id, syncState]);

  async function copy(value: string, target: 'key' | 'snippet') {
    try {
      await navigator.clipboard.writeText(value);
      setCopied(target);
    } catch {
      setCopied(null);
    }
  }

  async function findCost(requestId: string) {
    if (!requestId) return undefined;
    for (let attempt = 0; attempt < 3; attempt += 1) {
      if (attempt > 0) await wait(1000);
      try {
        const records = await api.get<UsageRecordsResponse>(
          `/v1/me/usage/records?keyId=${encodeURIComponent(created.details.id)}&limit=10&offset=0`,
        );
        const record = records.records.find((item) => item.requestId === requestId);
        if (record) return record.cost;
      } catch {
        return undefined;
      }
    }
    return undefined;
  }

  async function runTest() {
    setTest({ status: 'running' });
    try {
      const response = await fetch(`${site.apiBaseUrl.replace(/\/+$/, '')}/v1/messages`, {
        method: 'POST',
        headers: {
          'content-type': 'application/json',
          'anthropic-version': '2023-06-01',
          'x-api-key': created.key,
        },
        body: JSON.stringify({
          model: defaultModel,
          max_tokens: 32,
          messages: [{ role: 'user', content: 'Reply with exactly: NapKey ready' }],
        }),
      });
      const body: unknown = await response.json().catch(() => null);
      if (!response.ok) {
        const data = body && typeof body === 'object' ? (body as Record<string, unknown>) : {};
        const error = data.error && typeof data.error === 'object' ? data.error as Record<string, unknown> : {};
        throw new Error(typeof error.message === 'string' ? error.message : t('testHttpError', { status: response.status }));
      }
      const result = parseOnboardingResponse(body);
      if (!isValidOnboardingResponse(result)) throw new Error(t('invalidResponse'));
      const requestId = response.headers.get('x-request-id') ?? '';
      const cost = await findCost(requestId);
      setTest({
        status: 'success',
        text: result.text || t('testEmptyResult'),
        model: result.model || defaultModel,
        tokens: result.inputTokens + result.outputTokens,
        cost,
      });
    } catch (error) {
      setTest({ status: 'error', message: error instanceof Error ? error.message : t('testFailed') });
    }
  }

  const steps = [saved, saved, test.status === 'success', test.status === 'success'];

  return (
    <Panel as="section" className="overflow-hidden border-accent/40 bg-accent-soft">
      <PanelHeader title={t('title')} description={t('description')} />
      <ol className="grid border-b border-line sm:grid-cols-4">
        {['save', 'connect', 'test', 'usage'].map((step, index) => (
          <li key={step} className="flex items-center gap-2 border-line px-4 py-3 sm:border-r sm:last:border-r-0">
            <span className={`flex size-5 items-center justify-center rounded-full border font-mono text-[10px] ${steps[index] ? 'border-accent bg-accent text-bg' : 'border-line text-dim'}`}>
              {steps[index] ? '✓' : index + 1}
            </span>
            <span className="text-ui text-muted">{t(`steps.${step}`)}</span>
          </li>
        ))}
      </ol>

      <div className="grid min-w-0 lg:grid-cols-[minmax(0,0.78fr)_minmax(0,1.22fr)]">
        <div className="min-w-0 border-b border-line p-5 lg:border-r lg:border-b-0">
          <p className="font-mono text-label tracking-[0.12em] text-dim uppercase">01 / {t('saveLabel')}</p>
          <code className="mt-3 block max-w-full overflow-x-auto rounded-md border border-accent/30 bg-black/40 px-4 py-3 font-mono text-ui whitespace-nowrap text-accent-light">
            {created.key}
          </code>
          <button type="button" onClick={() => void copy(created.key, 'key')} className="mt-3 rounded-full bg-fg px-4 py-2 text-ui font-medium text-bg hover:bg-white/90">
            {copied === 'key' ? t('copied') : t('copyKey')}
          </button>
          <label className="mt-4 flex cursor-pointer items-start gap-3 text-ui text-muted">
            <input type="checkbox" checked={saved} onChange={(event) => setSaved(event.target.checked)} className="mt-0.5 size-4 accent-[var(--color-accent)]" />
            <span>{t('savedConfirm')}</span>
          </label>
          <p className="mt-3 text-ui leading-relaxed text-dim">{t('securityNote')}</p>
        </div>

        <div className="min-w-0 p-5">
          <p className="font-mono text-label tracking-[0.12em] text-dim uppercase">02 / {t('connectLabel')}</p>
          <div role="group" aria-label={t('toolLabel')} className="mt-3 flex max-w-full gap-2 overflow-x-auto pb-1 [scrollbar-width:thin]">
            {tools.map((item) => (
              <button key={item} type="button" aria-pressed={tool === item} onClick={() => setTool(item)} className={`shrink-0 rounded-full border px-3 py-1.5 text-ui whitespace-nowrap transition-colors ${tool === item ? 'border-accent/50 bg-accent-soft text-accent-light' : 'border-line text-muted hover:bg-white/5'}`}>
                {t(`tools.${item}`)}
              </button>
            ))}
          </div>
          <div className="mt-3 min-w-0 max-w-full overflow-hidden rounded-md border border-line bg-black/40">
            <div className="flex items-center justify-between border-b border-line px-4 py-2">
              <span className="font-mono text-label text-dim">{snippet.lang}</span>
              <button type="button" onClick={() => void copy(snippet.code, 'snippet')} className="text-ui text-muted hover:text-fg">
                {copied === 'snippet' ? t('copied') : t('copyConfig')}
              </button>
            </div>
            <pre className="max-h-72 max-w-full overflow-auto p-4 font-mono text-xs leading-relaxed text-muted"><code>{snippet.code}</code></pre>
          </div>

          <div className="mt-5 border-t border-line pt-5">
            <div className="flex flex-wrap items-center justify-between gap-3">
              <div>
                <p className="font-mono text-label tracking-[0.12em] text-dim uppercase">03 / {t('testLabel')}</p>
                <p className="mt-1 text-ui text-dim">{t('testCostWarning')}</p>
              </div>
              <button type="button" disabled={!saved || !synced || test.status === 'running'} onClick={() => void runTest()} className="rounded-full bg-fg px-5 py-2 text-ui font-medium text-bg hover:bg-white/90 disabled:pointer-events-none disabled:opacity-40">
                {test.status === 'running' ? t('testing') : t('runTest')}
              </button>
            </div>
            {syncState === 'pending' ? <p className="mt-3 text-ui text-info">{t('syncPending')}</p> : null}
            {syncState === 'failed' ? <p className="mt-3 text-ui text-danger">{t('syncFailed')}</p> : null}
            {test.status === 'error' ? <p role="alert" className="mt-3 rounded-md border border-danger/30 bg-danger-soft px-4 py-3 text-ui text-danger">{test.message}</p> : null}
            {test.status === 'success' ? (
              <div className="mt-3 rounded-md border border-accent/30 bg-black/30 p-4">
                <div className="flex flex-wrap items-center gap-2"><Badge tone="accent">{t('testPassed')}</Badge><span className="font-mono text-label text-dim">{test.model}</span></div>
                <p className="mt-3 font-mono text-sm text-fg">{test.text}</p>
                <div className="mt-3 flex flex-wrap gap-x-5 gap-y-1 text-ui text-dim">
                  <span>{t('tokens', { count: test.tokens })}</span>
                  <span>{test.cost ? t('cost', { value: test.cost.formatted }) : t('costPending')}</span>
                </div>
                <Link href="/console/usage" className="mt-3 inline-flex text-ui text-accent-light hover:underline">{t('viewUsage')} →</Link>
              </div>
            ) : null}
          </div>
        </div>
      </div>
      <div className="flex flex-wrap items-center justify-between gap-3 border-t border-line px-5 py-4">
        <p className="text-ui text-dim">{t('finishHint')}</p>
        <button type="button" disabled={!saved} onClick={onDone} className="rounded-full border border-line px-5 py-2 text-ui text-muted hover:bg-white/10 hover:text-fg disabled:pointer-events-none disabled:opacity-40">{t('finish')}</button>
      </div>
    </Panel>
  );
}
