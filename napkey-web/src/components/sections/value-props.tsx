import { useTranslations } from 'next-intl';
import { Section } from '@/components/ui/section';
import { Card } from '@/components/ui/card';

const cards = ['streaming', 'failover', 'oneKey'] as const;

export function ValueProps() {
  const t = useTranslations('value');

  return (
    <Section id="value" eyebrow={t('eyebrow')} title={t('title')}>
      <ul className="grid gap-6 md:grid-cols-3">
        {cards.map((key, index) => (
          <li key={key}>
            <Card className="h-full">
              <p
                aria-hidden
                className="mb-6 font-mono text-label tracking-[0.16em] text-dim tabular-nums"
              >
                {String(index + 1).padStart(2, '0')}
              </p>
              <h3 className="mb-3 text-2xl tracking-[-0.02em]">{t(`cards.${key}.title`)}</h3>
              <p className="text-ui leading-relaxed text-muted">{t(`cards.${key}.body`)}</p>
            </Card>
          </li>
        ))}
      </ul>
    </Section>
  );
}
