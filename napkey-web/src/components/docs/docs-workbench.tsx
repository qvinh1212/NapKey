'use client';

import { useMemo, useState } from 'react';
import { useTranslations } from 'next-intl';
import { developerSnippet, normalizeDeveloperModel, type DeveloperTool } from '@/lib/developer-tools';
import type { PublicModel } from '@/lib/model-catalog';
import { CopyButton } from '@/components/ui/copy-button';

const tools: Array<{ id: DeveloperTool; label: string }> = [
  { id: 'claudeCode', label: 'Claude Code' },
  { id: 'cursor', label: 'Cursor' },
  { id: 'cline', label: 'Cline / Roo' },
  { id: 'windsurf', label: 'Windsurf' },
  { id: 'langchain', label: 'LangChain' },
  { id: 'anthropic', label: 'Anthropic SDK' },
  { id: 'openai', label: 'OpenAI SDK' },
  { id: 'curl', label: 'cURL' },
  { id: 'powershell', label: 'PowerShell' },
];

export function DocsWorkbench({ models, apiBaseUrl }: { models: PublicModel[]; apiBaseUrl: string }) {
  const t = useTranslations('docsPage.quickstart');
  const [tool, setTool] = useState<DeveloperTool>('claudeCode');
  const [model, setModel] = useState(() => normalizeDeveloperModel('auto', models));
  const [apiKeyInput, setApiKeyInput] = useState('');

  const snippet = useMemo(
    () => developerSnippet(tool, model, apiBaseUrl, apiKeyInput),
    [apiBaseUrl, model, tool, apiKeyInput]
  );

  return (
    <div className="overflow-hidden rounded-xl border border-line bg-surface shadow-[0_20px_60px_rgba(0,0,0,0.5)]">
      <div className="grid gap-px bg-line lg:grid-cols-[0.8fr_1.2fr]">
        <div className="bg-surface p-5 sm:p-6">
          {/* Model Selector */}
          <label htmlFor="docs-model" className="font-mono text-label tracking-[0.12em] text-dim uppercase">
            {t('modelLabel')}
          </label>
          <select
            id="docs-model"
            value={model}
            onChange={(event) => setModel(event.target.value)}
            className="mt-2.5 w-full rounded-lg border border-line bg-black px-4 py-2.5 font-mono text-ui text-fg outline-none focus:border-accent"
          >
            {models.map((item) => (
              <option key={item.id} value={item.id}>
                {item.id}
              </option>
            ))}
          </select>

          {/* Interactive API Key Input */}
          <div className="mt-5">
            <label htmlFor="docs-apikey" className="font-mono text-label tracking-[0.12em] text-dim uppercase flex items-center justify-between">
              <span>Tự điền API Key (Tùy chọn)</span>
              {apiKeyInput ? (
                <button
                  type="button"
                  onClick={() => setApiKeyInput('')}
                  className="text-micro text-accent-light hover:underline font-mono"
                >
                  Xóa
                </button>
              ) : null}
            </label>
            <input
              id="docs-apikey"
              type="text"
              value={apiKeyInput}
              onChange={(e) => setApiKeyInput(e.target.value)}
              placeholder="nk_live_... hoặc để trống"
              className="mt-2.5 w-full rounded-lg border border-line bg-black px-4 py-2 font-mono text-label text-fg placeholder:text-dim outline-none focus:border-accent"
            />
          </div>

          {/* Client & IDE Tools Tabs */}
          <p className="mt-6 font-mono text-label tracking-[0.12em] text-dim uppercase">{t('toolLabel')}</p>
          <div role="group" aria-label={t('toolLabel')} className="mt-2.5 flex flex-wrap gap-1.5">
            {tools.map((item) => (
              <button
                key={item.id}
                type="button"
                aria-pressed={tool === item.id}
                onClick={() => setTool(item.id)}
                className={`rounded-lg border px-3 py-1.5 font-mono text-micro transition-all ${
                  tool === item.id
                    ? 'border-accent/50 bg-accent-soft text-accent-light font-semibold'
                    : 'border-line text-muted hover:border-line hover:text-fg'
                }`}
              >
                {item.label}
              </button>
            ))}
          </div>

          <div className="mt-6 rounded-lg border border-info/30 bg-info/10 p-4">
            <p className="text-ui font-medium text-info">{t('secretTitle')}</p>
            <p className="mt-1.5 text-ui leading-relaxed text-muted">{t('secretBody')}</p>
            <code className="mt-2 block font-mono text-xs text-fg">NAPKEY_API_KEY</code>
          </div>
        </div>

        {/* Dynamic Code Snippet Canvas */}
        <div className="min-w-0 bg-black/50 p-5 sm:p-6 flex flex-col justify-between">
          <div>
            <div className="flex flex-wrap items-center justify-between gap-3 border-b border-line pb-3">
              <div>
                <p className="font-mono text-label tracking-[0.12em] text-accent uppercase">{t('codeLabel')}</p>
                <p className="mt-0.5 text-micro font-mono text-dim">{t('codeHint')}</p>
              </div>
              <CopyButton
                value={snippet.code}
                label={t('copy')}
                copiedLabel={t('copied')}
                variant="pill"
                showTooltip
                className="py-1.5"
              />
            </div>
            <pre className="mt-4 max-h-[30rem] max-w-full overflow-auto rounded-lg border border-line bg-black/90 p-4 font-mono text-xs leading-relaxed text-zinc-300 sm:p-5">
              <code>{snippet.code}</code>
            </pre>
          </div>

          <div className="mt-4 flex items-center justify-between font-mono text-micro text-dim">
            <span>Model: {model}</span>
            <span className="text-accent-light">100% Sẵn sàng copy</span>
          </div>
        </div>
      </div>
    </div>
  );
}
