'use client';

import { useState } from 'react';
import { useTranslations } from 'next-intl';
import { Section } from '@/components/ui/section';
import { MODEL_TIERS } from '@/lib/pricing';
import { ENRICHED_MODELS, type ModelCapability } from '@/lib/model-metadata';
import { CopyButton } from '@/components/ui/copy-button';
import { PricingCalculator } from './pricing-calculator';
import { QuickConfigModal } from './quick-config-modal';

type FilterType = 'all' | ModelCapability;
type ViewMode = 'cards' | 'table';

export function PricingTable() {
  const t = useTranslations('pricing');
  const [filter, setFilter] = useState<FilterType>('all');
  const [view, setView] = useState<ViewMode>('cards');
  const [configTarget, setConfigTarget] = useState<{ id: string; name: string } | null>(null);

  const filteredModels = ENRICHED_MODELS.filter((model) => {
    if (filter === 'all') return true;
    return model.capabilities.includes(filter);
  });

  return (
    <Section id="pricing" eyebrow={t('eyebrow')} title={t('title')} subtitle={t('subtitle')}>
      {/* Quick Config Modal */}
      <QuickConfigModal
        open={Boolean(configTarget)}
        onClose={() => setConfigTarget(null)}
        modelId={configTarget?.id ?? ''}
        modelName={configTarget?.name ?? ''}
      />

      {/* Controls Bar: Capability Filter Pills + View Switcher */}
      <div className="mb-6 flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
        {/* Capability Filters */}
        <div className="flex flex-wrap items-center gap-2">
          <span className="font-mono text-label text-dim uppercase tracking-wider">
            {t('models.filterLabel')}
          </span>
          {(['all', 'coding', 'fast', 'thinking'] as const).map((key) => {
            const isActive = filter === key;
            return (
              <button
                key={key}
                type="button"
                onClick={() => setFilter(key)}
                className={`rounded-full border px-3 py-1 font-mono text-label transition-colors duration-150 ${
                  isActive
                    ? 'border-accent bg-accent-soft text-accent-light font-semibold'
                    : 'border-line text-muted hover:border-line hover:bg-surface-hover hover:text-fg'
                }`}
              >
                {t(`models.filters.${key}`)}
              </button>
            );
          })}
        </div>

        {/* View Mode Switcher (Cards / Table) */}
        <div
          role="group"
          aria-label="View switch"
          className="inline-flex self-start sm:self-auto items-center rounded-full border border-line bg-surface-hover p-0.5"
        >
          {(['cards', 'table'] as const).map((mode) => {
            const isActive = view === mode;
            return (
              <button
                key={mode}
                type="button"
                onClick={() => setView(mode)}
                className={`rounded-full px-3 py-1 font-mono text-label transition-colors duration-150 ${
                  isActive ? 'bg-white/10 text-fg font-medium' : 'text-dim hover:text-muted'
                }`}
              >
                {t(`models.views.${mode}`)}
              </button>
            );
          })}
        </div>
      </div>

      {/* View 1: Visual Model Cards */}
      {view === 'cards' ? (
        <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
          {filteredModels.map((model) => (
            <div
              key={model.id}
              className="group relative flex flex-col justify-between rounded-xl border border-line bg-surface p-5 transition-all duration-200 hover:border-accent/40 hover:bg-surface-hover"
            >
              <div>
                {/* Header: Verified Route Badge & Ratio */}
                <div className="flex items-center justify-between gap-2">
                  <div className="inline-flex items-center gap-1.5 font-mono text-micro text-accent-light">
                    <span className="size-1.5 rounded-full bg-accent animate-pulse" />
                    <span>{t('models.verifiedRoute')}</span>
                  </div>
                  <span className="rounded-full border border-line bg-white/5 px-2 py-0.5 font-mono text-micro text-muted">
                    {t('models.tierValue', { ratio: model.ratio })}
                  </span>
                </div>

                {/* Model Name & ID */}
                <div className="mt-3">
                  <h4 className="text-base font-semibold text-fg group-hover:text-accent-light transition-colors">
                    {model.name}
                  </h4>
                  <div className="mt-1 flex items-center gap-1.5">
                    <code className="font-mono text-micro text-dim truncate max-w-[170px]" title={model.id}>
                      {model.id}
                    </code>
                    <CopyButton value={model.id} variant="icon" showTooltip className="size-4" />
                  </div>
                </div>

                {/* Description / Recommended For */}
                <p className="mt-3 text-label text-muted leading-relaxed">
                  {model.recommendedFor}
                </p>

                {/* Feature Tags */}
                <div className="mt-3 flex flex-wrap gap-1.5">
                  {model.tags.map((tag) => (
                    <span
                      key={tag}
                      className="rounded border border-line/60 bg-black/40 px-1.5 py-0.5 font-mono text-micro text-dim"
                    >
                      {tag}
                    </span>
                  ))}
                </div>
              </div>

              {/* Price & Quick Setup Footer */}
              <div className="mt-5 border-t border-line/60 pt-3">
                <div className="flex items-center justify-between gap-2">
                  <div>
                    <div className="font-mono text-ui font-bold text-accent-light">
                      {t('models.ratePerMillion', { rate: model.pricePerMillion.toLocaleString('vi-VN') })}
                    </div>
                    <div className="font-mono text-micro text-dim mt-0.5">
                      Context: {model.contextWindow}
                    </div>
                  </div>
                  <button
                    type="button"
                    onClick={() => setConfigTarget({ id: model.id, name: model.name })}
                    className="shrink-0 rounded-full border border-line bg-surface-hover px-2.5 py-1 font-mono text-micro text-muted hover:border-accent/40 hover:bg-accent-soft hover:text-accent-light transition-all"
                  >
                    ⚡ {t('quickConfig.button')}
                  </button>
                </div>
              </div>
            </div>
          ))}
        </div>
      ) : (
        /* View 2: Detailed Technical Matrix Table */
        <div className="overflow-x-auto rounded-xl border border-line bg-surface">
          <table className="w-full text-left">
            <thead>
              <tr className="border-b border-line bg-bg/40">
                <th className="px-6 py-4 font-mono text-label tracking-[0.14em] text-dim uppercase">
                  {t('models.colModel')}
                </th>
                <th className="px-6 py-4 font-mono text-label tracking-[0.14em] text-dim uppercase">
                  Protocol
                </th>
                <th className="px-6 py-4 font-mono text-label tracking-[0.14em] text-dim uppercase">
                  Context
                </th>
                <th className="px-6 py-4 text-right font-mono text-label tracking-[0.14em] text-dim uppercase">
                  {t('models.colTier')}
                </th>
                <th className="px-6 py-4 text-right font-mono text-label tracking-[0.14em] text-accent-light uppercase">
                  {t('models.rateHeader')}
                </th>
                <th className="px-6 py-4 text-right font-mono text-label tracking-[0.14em] text-dim uppercase">
                  Action
                </th>
              </tr>
            </thead>
            <tbody>
              {filteredModels.map((model, index) => (
                <tr
                  key={model.id}
                  className={index < filteredModels.length - 1 ? 'border-b border-line/60' : ''}
                >
                  <td className="px-6 py-4">
                    <div className="flex items-center gap-2">
                      <span className="font-mono text-ui font-semibold text-fg">{model.name}</span>
                      <code className="font-mono text-micro text-dim">({model.id})</code>
                      <CopyButton value={model.id} variant="icon" showTooltip className="size-4" />
                    </div>
                  </td>
                  <td className="px-6 py-4 font-mono text-label text-muted">{model.family}</td>
                  <td className="px-6 py-4 font-mono text-label text-dim">{model.contextWindow}</td>
                  <td className="px-6 py-4 text-right font-mono text-ui tabular-nums text-muted">
                    {t('models.tierValue', { ratio: model.ratio })}
                  </td>
                  <td className="px-6 py-4 text-right font-mono text-ui font-semibold tabular-nums text-accent-light">
                    {t('models.ratePerMillion', { rate: model.pricePerMillion.toLocaleString('vi-VN') })}
                  </td>
                  <td className="px-6 py-4 text-right">
                    <button
                      type="button"
                      onClick={() => setConfigTarget({ id: model.id, name: model.name })}
                      className="rounded-full border border-line bg-surface-hover px-3 py-1 font-mono text-micro text-muted hover:border-accent/40 hover:bg-accent-soft hover:text-accent-light transition-all"
                    >
                      ⚡ {t('quickConfig.button')}
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      {/* Screen-reader Accessible Baseline Table for Contract Continuity */}
      <table className="sr-only" aria-label="Served models and pricing tiers">
        <thead>
          <tr>
            <th>{t('models.colModel')}</th>
            <th>{t('models.colTier')}</th>
          </tr>
        </thead>
        <tbody>
          {MODEL_TIERS.map((tier) => (
            <tr key={tier.id}>
              <td>{tier.id}</td>
              <td>{t('models.tierValue', { ratio: tier.ratio })}</td>
            </tr>
          ))}
        </tbody>
      </table>

      <p className="mt-3 text-prose text-dim">{t('models.note')}</p>

      {/* Interactive Cost Calculator & $20 Pro Comparison */}
      <PricingCalculator />

      <div className="relative mt-8 overflow-hidden rounded-xl border border-accent/40 bg-accent-soft p-5 sm:p-7">
        <div aria-hidden="true" className="absolute -right-12 -top-16 h-48 w-48 rounded-full bg-accent/15 blur-3xl" />
        <div className="relative grid gap-5 md:grid-cols-[1fr_auto] md:items-center">
          <div>
            <p className="font-mono text-micro uppercase tracking-[0.14em] text-accent-light">{t('offer.eyebrow')}</p>
            <h3 className="mt-2 font-display text-2xl font-bold text-fg sm:text-3xl">{t('offer.title')}</h3>
            <p className="mt-2 text-prose text-muted">{t('offer.body')}</p>
          </div>
          <div className="rounded-xl border border-accent/30 bg-bg/60 px-5 py-4 font-mono text-lg text-fg md:text-right">
            {t('offer.example')}
          </div>
        </div>
      </div>
    </Section>
  );
}
