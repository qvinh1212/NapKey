'use client';

import { useEffect, useRef, useState } from 'react';
import { useTranslations } from 'next-intl';
import { Section } from '@/components/ui/section';
import { snippets, type Snippet } from '@/lib/snippets';

export function Integration() {
  const t = useTranslations('integrate');
  const [active, setActive] = useState<Snippet['key']>('claudeCode');
  const [copied, setCopied] = useState(false);
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null);

  useEffect(
    () => () => {
      if (timer.current) clearTimeout(timer.current);
    },
    [],
  );

  const current = snippets.find((s) => s.key === active) ?? snippets[0]!;

  async function copy() {
    try {
      await navigator.clipboard.writeText(current.code);
      setCopied(true);
      if (timer.current) clearTimeout(timer.current);
      timer.current = setTimeout(() => setCopied(false), 2000);
    } catch {
      // Clipboard bi tu choi (khong phai HTTPS, hoac user chan quyen).
      // Code van hien tren man hinh nen nguoi dung chon tay duoc.
      setCopied(false);
    }
  }

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
                  onClick={() => {
                    setActive(snippet.key);
                    setCopied(false);
                  }}
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

          <button
            type="button"
            onClick={copy}
            aria-label={t('copyAria', { name: t(`tabs.${active}`) })}
            className="inline-flex min-h-11 items-center justify-center gap-2 self-end rounded-full border border-line px-4 py-1.5 font-mono text-label tracking-[0.08em] text-muted uppercase transition-colors duration-150 hover:text-fg sm:min-h-0"
          >
            <span aria-hidden>{copied ? '\u2713' : '\u29c9'}</span>
            {copied ? t('copied') : t('copy')}
          </button>
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

      <p aria-live="polite" className="sr-only">
        {copied ? t('copied') : ''}
      </p>

      <p className="mt-6 text-ui text-dim">{t('docsLink')}</p>
    </Section>
  );
}
