import { useTranslations } from 'next-intl';
import { Section } from '@/components/ui/section';
import { Card } from '@/components/ui/card';

const steps = ['topup', 'use', 'track'] as const;
const faqItems = ['refund', 'expiry', 'minimum', 'invoice'] as const;

export function Billing() {
  const t = useTranslations('billing');

  return (
    <Section id="billing" eyebrow={t('eyebrow')} title={t('title')} joined>
      <ol className="grid gap-6 md:grid-cols-3">
        {steps.map((key, index) => (
          <li key={key}>
            <Card className="h-full">
              <p
                aria-hidden
                className="mb-6 inline-flex size-8 items-center justify-center rounded-full border border-accent/40 bg-accent-soft font-mono text-label text-accent-light tabular-nums"
              >
                {index + 1}
              </p>
              <h3 className="mb-3 text-2xl tracking-[-0.02em]">{t(`steps.${key}.title`)}</h3>
              <p className="text-prose text-muted">{t(`steps.${key}.body`)}</p>
            </Card>
          </li>
        ))}
      </ol>

      <div className="mt-20">
        <h3 className="mb-8 text-2xl tracking-[-0.02em]">{t('faq.title')}</h3>
        <dl className="divide-y divide-line overflow-hidden rounded-lg border border-line bg-surface">
          {faqItems.map((key) => (
            <div key={key} className="grid gap-3 px-6 py-6 md:grid-cols-[18rem_1fr] md:gap-8">
              <dt className="text-base text-fg">{t(`faq.items.${key}.q`)}</dt>
              <dd className="text-prose text-muted">{t(`faq.items.${key}.a`)}</dd>
            </div>
          ))}
        </dl>
      </div>
    </Section>
  );
}
