import { useTranslations } from 'next-intl';
import { Link } from '@/i18n/navigation';
import { Section } from '@/components/ui/section';

const proofs = ['requestId', 'ledger', 'keys', 'prompts'] as const;

export function DeveloperTrust() {
  const t = useTranslations('trustSection');

  return (
    <Section id="trust" eyebrow={t('eyebrow')} title={t('title')} subtitle={t('subtitle')}>
      <div className="overflow-hidden rounded-2xl border border-line bg-surface">
        <div className="grid lg:grid-cols-[0.72fr_1.28fr]">
          <div className="border-b border-line p-6 lg:border-r lg:border-b-0 lg:p-8">
            <p className="font-mono text-label tracking-[0.16em] text-accent uppercase">{t('manifestLabel')}</p>
            <blockquote className="mt-5 text-2xl leading-snug tracking-[-0.02em] text-fg sm:text-3xl">
              {t('manifest')}
            </blockquote>
            <p className="mt-5 text-prose text-muted">{t('manifestBody')}</p>
            <div className="mt-7 flex flex-wrap gap-3">
              <Link href="/trust" className="rounded-lg bg-fg px-5 py-2.5 text-ui font-semibold text-bg transition-colors hover:bg-white/90">
                {t('readTrust')}
              </Link>
              <Link href="/status" className="rounded-lg border border-line px-5 py-2.5 text-ui text-muted transition-colors hover:bg-surface-2 hover:text-fg">
                {t('liveStatus')}
              </Link>
            </div>
          </div>

          <dl className="divide-y divide-line">
            {proofs.map((proof, index) => (
              <div key={proof} className="grid gap-2 px-6 py-5 sm:grid-cols-[3rem_12rem_1fr] sm:items-start sm:gap-5">
                <dt className="contents">
                  <span aria-hidden className="font-mono text-label text-dim">0{index + 1}</span>
                  <span className="text-sm font-medium text-fg">{t(`proofs.${proof}.title`)}</span>
                </dt>
                <dd className="text-prose text-muted">{t(`proofs.${proof}.body`)}</dd>
              </div>
            ))}
          </dl>
        </div>
      </div>
    </Section>
  );
}
