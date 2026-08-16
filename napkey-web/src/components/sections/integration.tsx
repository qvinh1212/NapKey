'use client';

import { useState } from 'react';
import { useTranslations } from 'next-intl';
import { Link } from '@/i18n/navigation';
import { Section } from '@/components/ui/section';
import { snippets, type Snippet } from '@/lib/snippets';
import { ArrowRightIcon } from '@/components/ui/icon';
import { CopyButton } from '@/components/ui/copy-button';

export function Integration() {
  const t = useTranslations('integrate');
  const [active, setActive] = useState<Snippet['key']>('claudeCode');

  const current = snippets.find((s) => s.key === active) ?? snippets[0]!;

  return (
    <Section id="integrate" eyebrow={t('eyebrow')} title={t('title')} subtitle={t('subtitle')}>
      <div className="overflow-hidden rounded-lg border border-line bg-surface">
        <div className="flex flex-col gap-3 border-b border-line px-4 py-3 sm:flex-row sm:items-center sm:justify-between">
          <div role="tablist" aria-label={t('eyebrow')} className="-mx-1 flex flex-nowrap gap-1 overflow-x-auto px-1 pb-1 [scrollbar-width:thin]">
            {snippets.map((snippet) => {
              const isActive = snippet.key === active;
              return (
                <button
                  key={snippet.key}
                  type="button"
                  role="tab"
                  id={`tab-${snippet.key}`}
                  aria-selected={isActive}
                  aria-controls={`panel-${snippet.key}`}
                  tabIndex={isActive ? 0 : -1}
                  onClick={() => setActive(snippet.key)}
                  className={
                    'shrink-0 rounded-full border px-3.5 py-1.5 text-ui whitespace-nowrap transition-colors duration-150 ' +
                    'ease-[var(--ease-smooth)] ' +
                    (isActive
                      ? 'border-accent bg-accent-soft text-accent-light'
                      : 'border-line text-muted hover:text-fg')
                  }
                >
                  {t(`tabs.${snippet.key}`)}
                </button>
              );
            })}
          </div>

          <CopyButton
            value={current.code}
            label={t('copy')}
            copiedLabel={t('copied')}
            variant="pill"
            showTooltip
            ariaLabel={t('copyAria', { name: t(`tabs.${active}`) })}
            className="self-end sm:min-h-0"
          />
        </div>

        {snippets.map((snippet) => (
          <div
            key={snippet.key}
            role="tabpanel"
            id={`panel-${snippet.key}`}
            aria-labelledby={`tab-${snippet.key}`}
            hidden={snippet.key !== active}
          >
            <pre className="overflow-x-auto px-4 py-5 font-mono text-xs leading-relaxed text-zinc-300 sm:px-6 sm:py-6 sm:text-ui">
              <code>{snippet.code}</code>
            </pre>
          </div>
        ))}
      </div>

      <Link href="/docs" className="mt-6 inline-flex text-ui text-accent-light hover:underline">
        {t('docsLink')}
        <ArrowRightIcon className="ml-1.5 size-3.5" />
      </Link>
    </Section>
  );
}
