'use client';

import { useLocale, useTranslations } from 'next-intl';
import { Section } from '@/components/ui/section';
import {
  MIN_TOPUP_VND,
  requestCostVnd,
  requestsFromVnd,
  requestShapes,
} from '@/lib/pricing';

export function PricingTable() {
  const t = useTranslations('pricing');
  const locale = useLocale();

  return (
    <Section id="pricing" eyebrow={t('eyebrow')} title={t('title')} subtitle={t('subtitle')}>
      <div className="grid gap-4 lg:grid-cols-3">
        {requestShapes.map((shape, index) => (
          <article
            key={shape.key}
            className={
              'relative overflow-hidden rounded-xl border p-6 ' +
              (index === 1
                ? 'border-accent/60 bg-accent-soft shadow-[0_0_40px_rgba(48,209,88,0.08)]'
                : 'border-line bg-surface')
            }
          >
            {index === 1 ? (
              <span className="absolute right-4 top-4 rounded-full border border-accent/40 px-2.5 py-1 font-mono text-micro uppercase tracking-[0.08em] text-accent-light">
                {t('popular')}
              </span>
            ) : null}
            <p className="text-ui text-muted">{t(`shapes.${shape.key}`)}</p>
            <p className="mt-5 font-mono text-4xl text-fg tabular-nums">
              {new Intl.NumberFormat(locale === 'vi' ? 'vi-VN' : 'en-US', {
                maximumFractionDigits: 0,
              }).format(requestCostVnd(shape.inputTokens, shape.outputTokens))}
              <span className="ml-2 text-base text-dim">VND</span>
            </p>
            <p className="mt-1 text-ui text-dim">{t('shapes.perRequest')}</p>
            <p className="mt-3 font-mono text-lg text-accent-light">
              {t('shapes.buys', { count: requestsFromVnd(MIN_TOPUP_VND, shape) })}
            </p>
            <p className="mt-5 border-t border-line/70 pt-4 text-ui leading-relaxed text-dim">
              {t('packageNote')}
            </p>
          </article>
        ))}
      </div>
      <p className="mt-3 text-ui leading-relaxed text-dim">{t('shapes.note')}</p>

      <div className="relative mt-6 overflow-hidden rounded-xl border border-accent/40 bg-accent-soft p-5 sm:p-7">
        <div aria-hidden="true" className="absolute -right-12 -top-16 h-48 w-48 rounded-full bg-accent/15 blur-3xl" />
        <div className="relative grid gap-5 md:grid-cols-[1fr_auto] md:items-center">
          <div>
            <p className="font-mono text-micro uppercase tracking-[0.14em] text-accent-light">{t('offer.eyebrow')}</p>
            <h3 className="mt-2 font-display text-2xl font-bold text-fg sm:text-3xl">{t('offer.title')}</h3>
            <p className="mt-2 text-ui leading-relaxed text-muted">{t('offer.body')}</p>
          </div>
          <div className="rounded-xl border border-accent/30 bg-bg/60 px-5 py-4 font-mono text-lg text-fg md:text-right">
            {t('offer.example')}
          </div>
        </div>
      </div>

      <div className="mt-6 grid gap-4 rounded-xl border border-line bg-surface p-5 md:grid-cols-[0.7fr_1.3fr] md:items-center">
        <div>
          <p className="font-mono text-micro uppercase tracking-[0.14em] text-dim">{t('rateLabel')}</p>
          <p className="mt-2 font-mono text-xl leading-snug text-fg">{t('rateValue')}</p>
        </div>
        <div className="space-y-2 text-ui leading-relaxed text-muted">
          <p>{t('measurement')}</p>
          <p>{t('feeNote')}</p>
          <p className="text-dim">{t('footnote')}</p>
        </div>
      </div>
    </Section>
  );
}
