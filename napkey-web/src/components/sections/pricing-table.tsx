'use client';

import { useTranslations } from 'next-intl';
import { Section } from '@/components/ui/section';
import { MODEL_TIERS, VND_PER_REQUEST } from '@/lib/pricing';

export function PricingTable() {
  const t = useTranslations('pricing');

  return (
    <Section id="pricing" eyebrow={t('eyebrow')} title={t('title')} subtitle={t('subtitle')}>
      <div className="overflow-hidden rounded-xl border border-line bg-surface">
        <table className="w-full text-left">
          <thead>
            <tr className="border-b border-line bg-bg/40">
              <th className="px-6 py-4 font-mono text-label tracking-[0.14em] text-dim uppercase">
                {t('models.colModel')}
              </th>
              <th className="px-6 py-4 text-right font-mono text-label tracking-[0.14em] text-dim uppercase">
                {t('models.colTier')}
              </th>
            </tr>
          </thead>
          <tbody>
            {MODEL_TIERS.map((tier, index) => (
              <tr
                key={tier.id}
                className={index < MODEL_TIERS.length - 1 ? 'border-b border-line/60' : ''}
              >
                <td className="px-6 py-4 font-mono text-ui text-fg">{tier.id}</td>
                <td className="px-6 py-4 text-right font-mono text-ui tabular-nums text-accent-light">
                  {t('models.tierValue', { ratio: tier.ratio })}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      <p className="mt-3 text-prose text-dim">{t('models.note')}</p>

      <div className="relative mt-6 overflow-hidden rounded-xl border border-accent/40 bg-accent-soft p-5 sm:p-7">
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

      <div className="mt-6 grid gap-4 rounded-xl border border-line bg-surface p-5 md:grid-cols-[0.7fr_1.3fr] md:items-center">
        <div>
          <p className="font-mono text-micro uppercase tracking-[0.14em] text-dim">{t('feeLabel')}</p>
          <p className="mt-2 font-mono text-xl leading-snug text-fg">
            {t('feeValue', { perRequest: VND_PER_REQUEST.toLocaleString('vi-VN') })}
          </p>
        </div>
        <div className="space-y-2 text-prose text-muted">
          <p>{t('measurement')}</p>
          <p>{t('feeNote')}</p>
          <p className="text-dim">{t('footnote')}</p>
        </div>
      </div>
    </Section>
  );
}
