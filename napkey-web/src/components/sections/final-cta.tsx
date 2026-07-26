import { useTranslations } from 'next-intl';
import { ButtonLink } from '@/components/ui/button';

export function FinalCta() {
  const t = useTranslations('cta');

  return (
    <section id="cta" aria-labelledby="cta-heading" className="section-y">
      <div className="container-page">
        <div className="relative overflow-hidden rounded-xl border border-line bg-surface px-8 py-20 text-center sm:px-16">
          <div
            aria-hidden
            className="pointer-events-none absolute inset-x-0 -top-32 mx-auto size-[32rem] rounded-full bg-accent/10 blur-[100px]"
          />
          <div className="relative">
            <h2
              id="cta-heading"
              className="mx-auto max-w-2xl text-3xl tracking-[-0.03em] sm:text-4xl lg:text-5xl"
            >
              {t('title')}
            </h2>
            <p className="mx-auto mt-6 max-w-xl text-base leading-relaxed text-muted">
              {t('body')}
            </p>
            <div className="mt-10 flex justify-center">
              <ButtonLink href="#pricing">{t('button')}</ButtonLink>
            </div>
          </div>
        </div>
      </div>
    </section>
  );
}
