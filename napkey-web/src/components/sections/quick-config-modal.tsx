'use client';

import { useEffect, useState } from 'react';
import { useTranslations } from 'next-intl';
import { site } from '@/lib/site';
import { developerSnippet, type DeveloperTool } from '@/lib/developer-tools';
import { CloseIcon } from '@/components/ui/icon';
import { CopyButton } from '@/components/ui/copy-button';

export interface QuickConfigModalProps {
  open: boolean;
  onClose: () => void;
  modelId: string;
  modelName: string;
}

const TOOLS: readonly { key: DeveloperTool; label: string }[] = [
  { key: 'claudeCode', label: 'Claude Code' },
  { key: 'cursor', label: 'Cursor' },
  { key: 'cline', label: 'Cline / Roo' },
  { key: 'windsurf', label: 'Windsurf' },
  { key: 'langchain', label: 'LangChain' },
  { key: 'anthropic', label: 'Anthropic SDK' },
  { key: 'openai', label: 'OpenAI SDK' },
  { key: 'curl', label: 'cURL' },
] as const;

export function QuickConfigModal({ open, onClose, modelId, modelName }: QuickConfigModalProps) {
  const t = useTranslations('pricing.quickConfig');
  const [selectedTool, setSelectedTool] = useState<DeveloperTool>('claudeCode');

  useEffect(() => {
    if (!open) return;
    function onKeyDown(event: KeyboardEvent) {
      if (event.key === 'Escape') onClose();
    }
    window.addEventListener('keydown', onKeyDown);
    return () => window.removeEventListener('keydown', onKeyDown);
  }, [open, onClose]);

  if (!open) return null;

  const snippet = developerSnippet(selectedTool, modelId, site.apiBaseUrl);

  return (
    <div
      role="dialog"
      aria-modal="true"
      aria-labelledby="quick-config-title"
      className="fixed inset-0 z-50 flex items-center justify-center p-4 sm:p-6"
    >
      {/* Backdrop */}
      <div
        onClick={onClose}
        aria-hidden="true"
        className="fixed inset-0 bg-black/80 backdrop-blur-sm transition-opacity"
      />

      {/* Modal Container */}
      <div className="relative z-10 max-h-[90vh] w-full max-w-xl overflow-hidden rounded-2xl border border-line bg-surface-3/95 shadow-2xl backdrop-blur-xl animate-[tooltip-spring_0.25s_cubic-bezier(0.34,1.56,0.64,1)_both]">
        {/* Header */}
        <div className="flex items-center justify-between border-b border-line px-6 py-4">
          <div>
            <div className="flex items-center gap-2">
              <span className="size-2 rounded-full bg-accent animate-pulse" />
              <h3 id="quick-config-title" className="text-base font-semibold text-fg sm:text-lg">
                {t('title', { model: modelName })}
              </h3>
            </div>
            <p className="mt-0.5 font-mono text-micro text-dim">
              Model ID: <span className="text-accent-light">{modelId}</span>
            </p>
          </div>
          <button
            type="button"
            onClick={onClose}
            aria-label="Close"
            className="rounded-full border border-line p-1.5 text-muted hover:bg-surface-2 hover:text-fg"
          >
            <CloseIcon className="size-4" />
          </button>
        </div>

        {/* Tool Switcher Tabs */}
        <div className="border-b border-line bg-surface/50 px-6 pt-3">
          <div role="tablist" aria-label="Tool options" className="-mx-1 flex flex-nowrap gap-1.5 overflow-x-auto pb-3 [scrollbar-width:thin]">
            {TOOLS.map(({ key, label }) => {
              const isActive = selectedTool === key;
              return (
                <button
                  key={key}
                  type="button"
                  role="tab"
                  aria-selected={isActive}
                  onClick={() => setSelectedTool(key)}
                  className={`shrink-0 rounded-full border px-3.5 py-1 font-mono text-label transition-colors ${
                    isActive
                      ? 'border-accent bg-accent-soft text-accent-light font-semibold'
                      : 'border-line text-muted hover:border-line hover:text-fg'
                  }`}
                >
                  {label}
                </button>
              );
            })}
          </div>
        </div>

        {/* Code Snippet Box */}
        <div className="p-6">
          <div className="relative overflow-hidden rounded-xl border border-line bg-terminal">
            <div className="flex items-center justify-between border-b border-line px-4 py-2 bg-surface-3">
              <span className="font-mono text-micro text-dim uppercase tracking-wider">
                {selectedTool === 'cursor' ? 'Cursor Configuration' : `${snippet.lang} snippet`}
              </span>
              <CopyButton
                value={snippet.code}
                variant="pill"
                showTooltip
                className="text-micro"
              />
            </div>
            <pre className="overflow-x-auto p-4 font-mono text-ui text-fg leading-relaxed [scrollbar-width:thin]">
              <code>{snippet.code}</code>
            </pre>
          </div>

          <div className="mt-4 flex items-center justify-between">
            <p className="text-label text-dim">
              {t('hint')}
            </p>
            <button
              type="button"
              onClick={onClose}
              className="rounded-full border border-line px-5 py-1.5 text-label font-medium text-muted hover:bg-surface-2 hover:text-fg"
            >
              {t('done')}
            </button>
          </div>
        </div>
      </div>
    </div>
  );
}
