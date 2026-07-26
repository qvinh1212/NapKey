import { useTranslations } from 'next-intl';
import { ButtonLink } from '@/components/ui/button';

export function Hero() {
  const t = useTranslations('hero');

  return (
    <section
      aria-labelledby="hero-heading"
      className="relative flex min-h-screen items-center overflow-hidden pt-32 pb-24"
    >
      <div aria-hidden className="grid-backdrop absolute inset-0 opacity-60" />
      <div
        aria-hidden
        className="absolute inset-x-0 top-0 h-px bg-gradient-to-r from-transparent via-white/20 to-transparent"
      />
      <div
        aria-hidden
        className="pointer-events-none absolute top-1/3 left-1/2 size-[42rem] -translate-x-1/2 rounded-full bg-accent/8 blur-[120px]"
      />

      <div className="container-page relative">
        <p className="mb-8 inline-flex items-center gap-2 rounded-full border border-line bg-surface-hover px-4 py-1.5 font-mono text-label tracking-[0.1em] text-accent-light">
          <span aria-hidden className="size-1.5 rounded-full bg-accent animate-pulse-dot" />
          {t('badge')}
        </p>

        <h1
          id="hero-heading"
          className="max-w-4xl text-5xl leading-[0.98] font-semibold tracking-[-0.045em] sm:text-6xl lg:text-[5.25rem]"
        >
          <span className="block text-gradient-fade">{t('titleLine1')}</span>
          <span className="block text-gradient-fade">{t('titleLine2')}</span>
        </h1>

        <p className="mt-8 max-w-2xl text-lg leading-relaxed text-muted">{t('subtitle')}</p>

        <div className="mt-11 flex flex-col gap-4 sm:flex-row sm:items-center">
          <ButtonLink href="#cta">{t('ctaPrimary')}</ButtonLink>
          <ButtonLink href="#integrate" variant="secondary">
            {t('ctaSecondary')}
          </ButtonLink>
        </div>

        <p className="mt-8 text-ui text-dim">{t('note')}</p>

        <dl className="mt-20 inline-flex items-center gap-3 rounded-full border border-line bg-surface px-5 py-2.5">
          <dt className="font-mono text-label tracking-[0.14em] text-dim uppercase">
            {t('statusLabel')}
          </dt>
          <dd className="flex items-center gap-2 text-ui text-accent-light">
            <span aria-hidden className="size-1.5 rounded-full bg-accent" />
            {t('statusValue')}
          </dd>
        </dl>
      </div>
    </section>
  );
}
