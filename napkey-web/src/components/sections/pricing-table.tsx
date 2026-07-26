'use client';

import { useMemo, useState } from 'react';
import { useLocale, useTranslations } from 'next-intl';
import { Section } from '@/components/ui/section';
import { formatVnd, modelFamilies, modelPrices, type ModelFamily } from '@/lib/pricing';

type Filter = ModelFamily | 'all';

export function PricingTable() {
  const t = useTranslations('pricing');
  const locale = useLocale();
  const [filter, setFilter] = useState<Filter>('all');

  const rows = useMemo(
    () => (filter === 'all' ? modelPrices : modelPrices.filter((m) => m.family === filter)),
    [filter],
  );

  const filters: readonly Filter[] = ['all', ...modelFamilies];

  return (
    <Section id="pricing" eyebrow={t('eyebrow')} title={t('title')} subtitle={t('subtitle')}>
      {/*
        Pill filter dung cap mau danger cua design system.
        Trang thai chon KHONG chi truyen dat bang mau: co aria-pressed
        va dau bullet dan dau, dat yeu cau khong dua vao mau don thuan.
      */}
      <div
        role="group"
        aria-label={t('filterAriaLabel')}
        className="mb-8 flex flex-wrap items-center gap-2"
      >
        {filters.map((key) => {
          const isActive = key === filter;
          return (
            <button
              key={key}
              type="button"
              aria-pressed={isActive}
              onClick={() => setFilter(key)}
              className={
                'inline-flex items-center gap-1.5 rounded-full border px-3.5 py-1.5 text-ui ' +
                'transition-colors duration-150 ease-[var(--ease-smooth)] ' +
                (isActive
                  ? 'border-danger bg-danger-soft text-danger'
                  : 'border-line text-muted hover:text-fg')
              }
            >
              {isActive ? <span aria-hidden>&bull;</span> : null}
              {key === 'all' ? t('filterAll') : t(`family.${key}`)}
            </button>
          );
        })}
      </div>

      <div className="overflow-x-auto rounded-lg border border-line bg-surface">
        <table className="w-full min-w-[46rem] border-collapse text-left">
          <caption className="sr-only">{t('table.caption')}</caption>
          <thead>
            <tr className="border-b border-line">
              <th scope="col" className="px-6 py-4 text-ui font-medium text-muted">
                {t('table.model')}
              </th>
              {(['input', 'output', 'cacheRead', 'cacheWrite'] as const).map((col) => (
                <th
                  key={col}
                  scope="col"
                  className="px-6 py-4 text-right text-ui font-medium text-muted"
                >
                  {t(`table.${col}`)}
                  <span className="mt-0.5 block font-mono text-micro tracking-[0.08em] text-dim">
                    {t('table.perMillion')}
                  </span>
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {rows.map((model) => (
              <tr
                key={model.id}
                className="border-b border-line/60 transition-colors duration-150 last:border-0 hover:bg-surface-hover"
              >
                <th scope="row" className="px-6 py-5 font-normal">
                  <span className="flex flex-wrap items-center gap-2">
                    <span className="text-base text-fg">{model.label}</span>
                    {model.tag ? (
                      <span className="rounded-full border border-accent/40 bg-accent-soft px-2 py-0.5 font-mono text-micro tracking-[0.08em] text-accent-light uppercase">
                        {t(`tag.${model.tag}`)}
                      </span>
                    ) : null}
                  </span>
                  <code className="mt-1.5 block font-mono text-label text-dim">{model.id}</code>
                </th>
                <td className="px-6 py-5 text-right font-mono text-ui text-zinc-300 tabular-nums">
                  {formatVnd(model.inputVnd, locale)}
                </td>
                <td className="px-6 py-5 text-right font-mono text-ui text-zinc-300 tabular-nums">
                  {formatVnd(model.outputVnd, locale)}
                </td>
                <td className="px-6 py-5 text-right font-mono text-ui text-accent-light tabular-nums">
                  {formatVnd(model.cacheReadVnd, locale)}
                </td>
                <td className="px-6 py-5 text-right font-mono text-ui text-zinc-300 tabular-nums">
                  {formatVnd(model.cacheWriteVnd, locale)}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      <div className="mt-6 space-y-2">
        <p className="text-ui text-muted">{t('thinkingNote')}</p>
        <p className="max-w-3xl text-label leading-relaxed text-dim">{t('footnote')}</p>
      </div>
    </Section>
  );
}
