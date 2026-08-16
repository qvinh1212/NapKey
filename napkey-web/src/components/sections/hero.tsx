import { useTranslations } from 'next-intl';
import { ButtonLink } from '@/components/ui/button';
import { site } from '@/lib/site';
import { ArrowUpRightIcon } from '@/components/ui/icon';

export function Hero() {
  const t = useTranslations('hero');
  const setupLines = [
    ['muted', `# ${t('terminalComment')}`],
    ['plain', `export ANTHROPIC_BASE_URL="${site.apiBaseUrl}"`],
    ['accent', 'export ANTHROPIC_AUTH_TOKEN="nk_live_..."'],
    ['plain', 'claude'],
  ] as const;

  return (
    <section aria-labelledby="hero-heading" className="relative overflow-hidden pt-28 pb-16 sm:pt-36 sm:pb-20 lg:pt-44 lg:pb-24">
      <div aria-hidden className="grid-backdrop absolute inset-0 opacity-55" />
      <div aria-hidden className="hero-signal absolute inset-0" />
      <div
        aria-hidden
        className="absolute inset-x-0 top-0 h-px bg-gradient-to-r from-transparent via-white/20 to-transparent"
      />

      <div className="container-page relative">
        <div className="grid items-center gap-10 sm:gap-16 lg:grid-cols-[minmax(0,1.05fr)_minmax(28rem,0.95fr)] lg:gap-12">
          <div className="animate-rise">
            <p className="mb-7 inline-flex items-center gap-2 rounded-full border border-accent/30 bg-accent-soft px-4 py-1.5 font-mono text-label tracking-[0.1em] text-accent-light">
              <span aria-hidden className="size-1.5 rounded-full bg-accent animate-pulse-dot" />
              {t('badge')}
            </p>

            <h1
              id="hero-heading"
              className="max-w-3xl text-[clamp(2.75rem,14vw,4.5rem)] leading-[0.94] font-semibold tracking-[-0.055em] lg:text-[5.5rem]"
            >
              <span className="block text-gradient-fade">{t('titleLine1')}</span>
              <span className="block text-gradient-fade">{t('titleLine2')}</span>
            </h1>

            <p className="mt-6 max-w-xl text-base leading-relaxed text-muted sm:mt-8 sm:text-xl">{t('subtitle')}</p>

            <div className="mt-8 flex flex-col gap-3 sm:mt-10 sm:flex-row sm:items-center sm:gap-4">
              <ButtonLink href="/signup">{t('ctaPrimary')}</ButtonLink>
              <ButtonLink href="#integrate" variant="secondary">
                {t('ctaSecondary')}
              </ButtonLink>
            </div>

            <p className="mt-6 max-w-lg text-ui leading-relaxed text-muted">{t('note')}</p>
          </div>

          <div className="relative animate-rise [animation-delay:120ms]">
            <div aria-hidden className="absolute -inset-8 bg-accent/6 blur-3xl" />
            <div className="relative overflow-hidden rounded-xl border border-white/15 bg-[#070908]/90 shadow-[0_32px_100px_rgba(0,0,0,0.65)] backdrop-blur">
              <div className="flex items-center justify-between border-b border-line px-5 py-4">
                <div className="flex items-center gap-3">
                  <div className="flex gap-2" aria-hidden>
                    <span className="size-2.5 rounded-full bg-[#ff5f57]" />
                    <span className="size-2.5 rounded-full bg-[#febc2e]" />
                    <span className="size-2.5 rounded-full bg-[#28c840]" />
                  </div>
                  <div className="hidden sm:inline-flex items-center gap-1.5 rounded-full border border-accent/30 bg-accent-soft px-2 py-0.5 font-mono text-micro text-accent-light">
                    <span className="size-1.5 rounded-full bg-accent animate-pulse" />
                    <span>~310ms TTFT · 115 tok/s</span>
                  </div>
                </div>
                <span className="font-mono text-micro tracking-[0.14em] text-muted uppercase">
                  {t('terminalLabel')}
                </span>
              </div>

              <div className="min-h-64 px-5 py-7 sm:px-7">
                <div className="mb-7 flex items-center gap-3 border-b border-line pb-5">
                  <span className="flex size-8 items-center justify-center rounded-md border border-accent/30 bg-accent-soft font-mono text-label text-accent-light">
                    NK
                  </span>
                  <div>
                    <p className="font-mono text-ui text-zinc-200">{t('terminalTitle')}</p>
                    <p className="mt-0.5 font-mono text-micro text-muted">{t('terminalSubtitle')}</p>
                  </div>
                  <span className="ml-auto inline-flex items-center gap-2 font-mono text-micro text-accent-light">
                    <span aria-hidden className="size-1.5 rounded-full bg-accent" />
                    {t('terminalReady')}
                  </span>
                </div>

                <pre className="overflow-x-auto font-mono text-[0.72rem] leading-7 sm:text-ui">
                  <code>
                    {setupLines.map(([tone, line]) => (
                      <span
                        key={line}
                        className={
                          'block ' +
                          (tone === 'accent'
                            ? 'text-accent-light'
                            : tone === 'muted'
                              ? 'text-muted'
                              : 'text-zinc-300')
                        }
                      >
                        <span aria-hidden className="mr-3 select-none text-zinc-700">$</span>
                        {line}
                      </span>
                    ))}
                  </code>
                </pre>
              </div>

              <div className="grid grid-cols-3 border-t border-line bg-white/[0.015]">
                {(['topUp', 'protocols', 'monthlyFee'] as const).map((key) => (
                  <div key={key} className="min-w-0 border-r border-line px-2.5 py-4 last:border-r-0 sm:px-5">
                    <p className="font-display text-lg font-semibold tracking-[-0.04em] text-fg sm:text-2xl">
                      {t(`metrics.${key}.value`)}
                    </p>
                    <p className="mt-1 text-micro leading-snug text-muted sm:text-label">
                      {t(`metrics.${key}.label`)}
                    </p>
                  </div>
                ))}
              </div>
            </div>
          </div>
        </div>

        <div className="mt-14 border-y border-line py-6 sm:mt-20">
          <div className="flex flex-col gap-5 lg:flex-row lg:items-center lg:justify-between">
            <p className="font-mono text-label tracking-[0.14em] text-muted uppercase">{t('worksWith')}</p>
            <ul className="grid grid-cols-2 gap-x-8 gap-y-4 text-sm text-subtle sm:grid-cols-4 lg:flex lg:items-center lg:gap-10">
              {(['claudeCode', 'cursor', 'anthropicSdk', 'openaiSdk'] as const).map((tool) => (
                <li key={tool} className="flex items-center gap-2.5">
                  <span aria-hidden className="size-1.5 rounded-full bg-accent/80" />
                  {t(`tools.${tool}`)}
                </li>
              ))}
            </ul>
            <a
              href={`${site.apiBaseUrl}/health`}
              className="inline-flex items-center gap-2 text-ui text-muted transition-colors hover:text-fg"
            >
              <span aria-hidden className="size-1.5 rounded-full bg-accent animate-pulse-dot" />
              {t('statusLink')}
              <ArrowUpRightIcon className="size-3.5" />
            </a>
          </div>
        </div>
      </div>
    </section>
  );
}
