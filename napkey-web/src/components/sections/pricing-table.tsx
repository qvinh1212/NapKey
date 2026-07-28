'use client';

import { useLocale, useTranslations } from 'next-intl';
import { Section } from '@/components/ui/section';
import { creditPackages, formatVnd, VND_PER_CREDIT } from '@/lib/pricing';

export function PricingTable() {
  const t = useTranslations('pricing');
  const locale = useLocale();

  return (
    <Section id="pricing" eyebrow={t('eyebrow')} title={t('title')} subtitle={t('subtitle')}>
      <div className="grid gap-4 lg:grid-cols-3">
        {creditPackages.map((pack, index) => (
          <article
            key={pack.credits}
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
            <p className="text-ui text-muted">{t(`packages.${pack.key}`)}</p>
            <p className="mt-5 font-mono text-4xl text-fg tabular-nums">
              {new Intl.NumberFormat(locale === 'vi' ? 'vi-VN' : 'en-US').format(pack.credits)}
              <span className="ml-2 text-base text-dim">credit</span>
            </p>
            <p className="mt-3 font-mono text-lg text-accent-light">{formatVnd(pack.vnd, locale)}</p>
            <p className="mt-5 border-t border-line/70 pt-4 text-ui leading-relaxed text-dim">
              {t('packageNote')}
            </p>
          </article>
        ))}
      </div>

      <div className="mt-6 grid gap-4 rounded-xl border border-line bg-surface p-5 md:grid-cols-[0.7fr_1.3fr] md:items-center">
        <div>
          <p className="font-mono text-micro uppercase tracking-[0.14em] text-dim">{t('rateLabel')}</p>
          <p className="mt-2 font-mono text-2xl text-fg">1 credit = {VND_PER_CREDIT} VND</p>
        </div>
        <div className="space-y-2 text-ui leading-relaxed text-muted">
          <p>{t('measurement')}</p>
          <p className="text-dim">{t('footnote')}</p>
        </div>
      </div>
    </Section>
  );
}
