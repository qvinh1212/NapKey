import { useTranslations } from 'next-intl';
import { Link } from '@/i18n/navigation';
import { Section } from '@/components/ui/section';

const protocols = ['anthropic', 'openai', 'responses'] as const;

export function Compatibility() {
  const t = useTranslations('compatibilitySection');

  return (
    <Section id="compatibility" eyebrow={t('eyebrow')} title={t('title')} subtitle={t('subtitle')} joined>
      <div className="overflow-hidden rounded-2xl border border-line bg-surface">
        <div className="grid lg:grid-cols-[1.1fr_0.9fr]">
          <div className="border-b border-line p-6 sm:p-8 lg:border-r lg:border-b-0">
            <p className="font-mono text-label tracking-[0.16em] text-accent uppercase">{t('contractLabel')}</p>
            <p className="mt-5 max-w-2xl text-2xl leading-snug tracking-[-0.02em] text-fg sm:text-3xl">{t('contract')}</p>
            <p className="mt-5 max-w-2xl text-prose text-muted">{t('body')}</p>
            <Link href="/compatibility" className="mt-7 inline-flex rounded-lg bg-fg px-5 py-2.5 text-ui font-semibold text-bg transition-colors hover:bg-white/90">
              {t('cta')}
            </Link>
          </div>
          <dl className="divide-y divide-line">
            {protocols.map((protocol, index) => (
              <div key={protocol} className="grid grid-cols-[2.5rem_1fr] gap-3 px-6 py-5 sm:px-8">
                <dt className="contents">
                  <span className="font-mono text-label text-dim">0{index + 1}</span>
                  <span className="text-sm font-medium text-fg">{t(`protocols.${protocol}.title`)}</span>
                </dt>
                <dd className="col-start-2 text-prose text-muted">{t(`protocols.${protocol}.body`)}</dd>
              </div>
            ))}
          </dl>
        </div>
      </div>
    </Section>
  );
}
