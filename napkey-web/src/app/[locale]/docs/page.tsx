import type { Metadata } from 'next';
import { getTranslations, setRequestLocale } from 'next-intl/server';
import type { Locale } from '@/i18n/routing';
import { Link } from '@/i18n/navigation';
import { DocsWorkbench } from '@/components/docs/docs-workbench';
import { diagnoseApiFailure } from '@/lib/developer-tools';
import { readModelCatalog } from '@/lib/model-catalog';
import { publicPageMetadata } from '@/lib/public-metadata';
import { site } from '@/lib/site';

const sections = ['quickstart', 'api', 'models', 'billing', 'errors'] as const;
const endpoints = ['messages', 'chat', 'countTokens', 'models'] as const;
const errorStatuses = [400, 401, 402, 429, 503] as const;

export async function generateMetadata({ params }: { params: Promise<{ locale: string }> }): Promise<Metadata> {
  const { locale } = await params;
  const t = await getTranslations({ locale, namespace: 'docsPage' });
  return publicPageMetadata(locale, 'docs', t('metaTitle'), t('intro'));
}

export default async function DocsPage({ params }: { params: Promise<{ locale: string }> }) {
  const { locale } = await params;
  setRequestLocale(locale as Locale);
  const [t, catalog] = await Promise.all([getTranslations('docsPage'), readModelCatalog()]);
  const apiBaseUrl = site.apiBaseUrl.replace(/\/+$/, '');

  return (
    <main id="main" className="min-h-screen bg-black pt-28 pb-24 sm:pt-32">
      <div className="container-page">
        <header className="border-b border-line pb-12 sm:pb-16">
          <p className="font-mono text-label tracking-[0.18em] text-accent uppercase">{t('eyebrow')}</p>
          <div className="mt-5 grid gap-7 lg:grid-cols-[minmax(0,1fr)_22rem] lg:items-end">
            <div>
              <h1 className="max-w-4xl text-4xl leading-[1.04] tracking-[-0.04em] text-balance sm:text-6xl">{t('title')}</h1>
              <p className="mt-6 max-w-3xl text-base leading-relaxed text-muted sm:text-lg">{t('intro')}</p>
            </div>
            <div className="rounded-lg border border-accent/30 bg-accent-soft p-5">
              <p className="font-mono text-label tracking-[0.12em] text-accent-light uppercase">{t('endpointLabel')}</p>
              <code className="mt-3 block overflow-x-auto font-mono text-sm text-fg">{apiBaseUrl}</code>
              <p className="mt-3 text-ui leading-relaxed text-muted">{t('endpointHint')}</p>
            </div>
          </div>
        </header>

        <div className="grid gap-10 pt-10 lg:grid-cols-[13rem_minmax(0,1fr)] lg:gap-14">
          <aside className="lg:sticky lg:top-28 lg:h-fit">
            <p className="font-mono text-label tracking-[0.14em] text-dim uppercase">{t('contents')}</p>
            <nav aria-label={t('contents')} className="mt-4 flex gap-2 overflow-x-auto pb-2 lg:flex-col lg:overflow-visible">
              {sections.map((section, index) => (
                <a key={section} href={`#${section}`} className="shrink-0 rounded-md border border-line px-3 py-2 text-ui text-muted transition-colors hover:border-accent/40 hover:bg-accent-soft hover:text-fg lg:border-transparent">
                  <span className="mr-2 font-mono text-micro text-dim">0{index + 1}</span>{t(`nav.${section}`)}
                </a>
              ))}
            </nav>
            <Link href="/signup" className="mt-6 hidden w-full rounded-full bg-fg px-4 py-2.5 text-center text-ui font-medium text-bg lg:block">{t('createKey')}</Link>
          </aside>

          <div className="min-w-0">
            <section id="quickstart" className="scroll-mt-28 pb-16">
              <SectionHeading number="01" title={t('quickstart.title')} description={t('quickstart.description')} />
              <ol className="mt-8 grid gap-px overflow-hidden rounded-lg border border-line bg-line sm:grid-cols-3">
                {(['account', 'key', 'request'] as const).map((step, index) => (
                  <li key={step} className="bg-surface p-5">
                    <span className="font-mono text-label text-accent">0{index + 1}</span>
                    <h3 className="mt-3 text-base text-fg">{t(`quickstart.steps.${step}.title`)}</h3>
                    <p className="mt-2 text-ui leading-relaxed text-muted">{t(`quickstart.steps.${step}.body`)}</p>
                  </li>
                ))}
              </ol>
              <div className="mt-8"><DocsWorkbench models={catalog.models} apiBaseUrl={apiBaseUrl} /></div>
            </section>

            <section id="api" className="scroll-mt-28 border-t border-line py-16">
              <SectionHeading number="02" title={t('api.title')} description={t('api.description')} />
              <div className="mt-8 divide-y divide-line overflow-hidden rounded-lg border border-line">
                {endpoints.map((endpoint) => (
                  <article key={endpoint} className="grid gap-3 bg-surface p-5 sm:grid-cols-[13rem_1fr] sm:p-6">
                    <code className="font-mono text-sm text-accent-light">{t(`api.endpoints.${endpoint}.path`)}</code>
                    <div><h3 className="text-base text-fg">{t(`api.endpoints.${endpoint}.title`)}</h3><p className="mt-2 text-ui leading-relaxed text-muted">{t(`api.endpoints.${endpoint}.body`)}</p></div>
                  </article>
                ))}
              </div>
              <div className="mt-6 rounded-lg border border-info/30 bg-info/10 p-5 text-ui leading-relaxed text-muted"><strong className="text-info">{t('api.authTitle')}</strong> {t('api.authBody')}</div>
            </section>

            <section id="models" className="scroll-mt-28 border-t border-line py-16">
              <SectionHeading number="03" title={t('models.title')} description={t('models.description')} />
              <div className="mt-8 flex items-center gap-2 font-mono text-label text-muted"><span className={`size-2 rounded-full ${catalog.live ? 'bg-accent' : 'bg-warn'}`} />{catalog.live ? t('models.live') : t('models.fallback')}</div>
              <ul className="mt-4 grid overflow-hidden rounded-lg border border-line sm:grid-cols-2 xl:grid-cols-3">
                {catalog.models.map((model) => <li key={model.id} className="min-w-0 border-b border-line p-4 sm:border-r"><code className="block truncate font-mono text-ui text-fg" title={model.id}>{model.id}</code><span className="mt-2 inline-flex rounded-full bg-white/5 px-2.5 py-1 font-mono text-micro text-muted uppercase">{model.family}</span></li>)}
              </ul>
            </section>

            <section id="billing" className="scroll-mt-28 border-t border-line py-16">
              <SectionHeading number="04" title={t('billing.title')} description={t('billing.description')} />
              <div className="mt-8 grid gap-px overflow-hidden rounded-lg border border-line bg-line sm:grid-cols-2">
                {(['price', 'minimum', 'trial', 'bonus'] as const).map((item) => <article key={item} className="bg-surface p-6"><p className="font-mono text-xl text-accent-light">{t(`billing.cards.${item}.value`)}</p><h3 className="mt-3 text-base text-fg">{t(`billing.cards.${item}.title`)}</h3><p className="mt-2 text-ui leading-relaxed text-muted">{t(`billing.cards.${item}.body`)}</p></article>)}
              </div>
            </section>

            <section id="errors" className="scroll-mt-28 border-t border-line pt-16">
              <SectionHeading number="05" title={t('errors.title')} description={t('errors.description')} />
              <div className="mt-8 divide-y divide-line overflow-hidden rounded-lg border border-line">
                {errorStatuses.map((status) => {
                  const diagnosis = diagnoseApiFailure(status);
                  return <article key={status} className="grid gap-3 bg-surface p-5 sm:grid-cols-[7rem_1fr] sm:p-6"><code className="font-mono text-lg text-fg">HTTP {status}</code><div><h3 className="text-base text-fg">{t(`errors.items.${diagnosis.key}.title`)}</h3><p className="mt-2 text-ui leading-relaxed text-muted">{t(`errors.items.${diagnosis.key}.body`)}</p></div></article>;
                })}
              </div>
              <div className="mt-8 flex flex-wrap gap-3"><Link href="/status" className="rounded-full border border-line px-5 py-2.5 text-ui text-muted hover:text-fg">{t('errors.status')}</Link><Link href="/console/usage" className="rounded-full border border-line px-5 py-2.5 text-ui text-muted hover:text-fg">{t('errors.usage')}</Link><Link href="/signup" className="rounded-full bg-fg px-5 py-2.5 text-ui font-medium text-bg">{t('createKey')}</Link></div>
            </section>
          </div>
        </div>
      </div>
    </main>
  );
}

function SectionHeading({ number, title, description }: { number: string; title: string; description: string }) {
  return <header><p className="font-mono text-label tracking-[0.16em] text-accent uppercase">{number}</p><h2 className="mt-3 text-3xl tracking-[-0.025em] text-fg sm:text-4xl">{title}</h2><p className="mt-4 max-w-3xl text-ui leading-relaxed text-muted sm:text-base">{description}</p></header>;
}

