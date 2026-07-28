import type { Metadata } from 'next';
import { getTranslations, setRequestLocale } from 'next-intl/server';
import type { Locale } from '@/i18n/routing';
import { Link } from '@/i18n/navigation';
import { publicPageMetadata } from '@/lib/public-metadata';
import { readModelCatalog } from '@/lib/model-catalog';
import { site } from '@/lib/site';

const capabilities = ['messages', 'chat', 'responses', 'streaming', 'tools', 'vision', 'thinking', 'countTokens'] as const;
const playbooks = ['claudeCode', 'anthropic', 'openai'] as const;

export async function generateMetadata({ params }: { params: Promise<{ locale: string }> }): Promise<Metadata> {
  const { locale } = await params;
  const t = await getTranslations({ locale, namespace: 'compatibilityPage' });
  return publicPageMetadata(locale, 'compatibility', t('metaTitle'), t('intro'));
}

export default async function CompatibilityPage({ params }: { params: Promise<{ locale: string }> }) {
  const { locale } = await params;
  setRequestLocale(locale as Locale);
  const [t, catalog] = await Promise.all([getTranslations('compatibilityPage'), readModelCatalog()]);
  const apiBaseUrl = site.apiBaseUrl.replace(/\/+$/, '');

  return (
    <div className="min-h-screen bg-black pt-32 pb-24">
      <div className="container-page">
        <header className="max-w-4xl border-b border-line pb-14">
          <p className="font-mono text-label tracking-[0.18em] text-accent uppercase">{t('eyebrow')}</p>
          <h1 className="mt-5 text-4xl leading-[1.04] tracking-[-0.035em] sm:text-6xl">{t('title')}</h1>
          <p className="mt-6 max-w-3xl text-base leading-relaxed text-muted sm:text-lg">{t('intro')}</p>
          <div className="mt-8 flex flex-wrap gap-3">
            <Link href="/signup" className="rounded-full bg-fg px-5 py-2.5 text-ui font-medium text-bg">{t('createKey')}</Link>
            <a href={`${apiBaseUrl}/v1/models`} className="rounded-full border border-line px-5 py-2.5 text-ui text-muted hover:text-fg">GET /v1/models</a>
          </div>
        </header>

        <section aria-labelledby="catalog-heading" className="py-16">
          <div className="flex flex-col gap-4 sm:flex-row sm:items-end sm:justify-between">
            <div>
              <p className="font-mono text-label tracking-[0.16em] text-accent uppercase">01 / {t('catalog.eyebrow')}</p>
              <h2 id="catalog-heading" className="mt-3 text-3xl sm:text-4xl">{t('catalog.title')}</h2>
            </div>
            <p className="flex items-center gap-2 font-mono text-label text-muted">
              <span aria-hidden className={`size-2 rounded-full ${catalog.live ? 'animate-pulse-dot bg-accent' : 'bg-warn'}`} />
              {catalog.live ? t('catalog.live') : t('catalog.fallback')}
            </p>
          </div>
          <p className="mt-4 max-w-3xl text-ui leading-relaxed text-muted">{t('catalog.scope')}</p>
          <ul className="mt-8 grid overflow-hidden rounded-lg border border-line sm:grid-cols-2 lg:grid-cols-3">
            {catalog.models.map((model) => (
              <li key={model.id} className="min-w-0 border-b border-line p-5 sm:border-r [&:last-child]:border-b-0">
                <code className="block truncate font-mono text-sm text-fg" title={model.id}>{model.id}</code>
                <div className="mt-3 flex flex-wrap gap-2">
                  <span className="rounded-full bg-white/5 px-2.5 py-1 font-mono text-micro tracking-wide text-muted uppercase">{model.family}</span>
                  {model.thinking ? <span className="rounded-full bg-accent-soft px-2.5 py-1 font-mono text-micro tracking-wide text-accent-light uppercase">thinking</span> : null}
                </div>
              </li>
            ))}
          </ul>
        </section>

        <section aria-labelledby="matrix-heading" className="border-t border-line py-16">
          <p className="font-mono text-label tracking-[0.16em] text-accent uppercase">02 / {t('matrix.eyebrow')}</p>
          <h2 id="matrix-heading" className="mt-3 text-3xl sm:text-4xl">{t('matrix.title')}</h2>
          <p className="mt-4 max-w-3xl text-ui leading-relaxed text-muted">{t('matrix.scope')}</p>
          <div className="mt-8 overflow-x-auto rounded-lg border border-line">
            <table className="w-full min-w-[720px] border-collapse text-left text-ui">
              <caption className="sr-only">{t('matrix.caption')}</caption>
              <thead className="bg-white/[0.03] font-mono text-label tracking-wide text-dim uppercase">
                <tr><th className="px-5 py-4">{t('matrix.feature')}</th><th className="px-5 py-4">Anthropic</th><th className="px-5 py-4">OpenAI</th><th className="px-5 py-4">{t('matrix.behavior')}</th></tr>
              </thead>
              <tbody className="divide-y divide-line">
                {capabilities.map((capability) => (
                  <tr key={capability}>
                    <th scope="row" className="px-5 py-4 font-medium text-fg">{t(`matrix.rows.${capability}.name`)}</th>
                    <td className="px-5 py-4 text-accent-light">{t(`matrix.rows.${capability}.anthropic`)}</td>
                    <td className="px-5 py-4 text-accent-light">{t(`matrix.rows.${capability}.openai`)}</td>
                    <td className="max-w-md px-5 py-4 text-muted">{t(`matrix.rows.${capability}.note`)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </section>

        <section aria-labelledby="migration-heading" className="border-t border-line py-16">
          <p className="font-mono text-label tracking-[0.16em] text-accent uppercase">03 / {t('migration.eyebrow')}</p>
          <h2 id="migration-heading" className="mt-3 text-3xl sm:text-4xl">{t('migration.title')}</h2>
          <p className="mt-4 max-w-3xl text-ui leading-relaxed text-muted">{t('migration.scope')}</p>
          <div className="mt-8 grid gap-px overflow-hidden rounded-lg border border-line bg-line lg:grid-cols-3">
            {playbooks.map((playbook, index) => (
              <article key={playbook} className="min-w-0 bg-black p-6">
                <p className="font-mono text-label text-dim">0{index + 1}</p>
                <h3 className="mt-4 text-xl text-fg">{t(`migration.playbooks.${playbook}.title`)}</h3>
                <p className="mt-3 text-ui leading-relaxed text-muted">{t(`migration.playbooks.${playbook}.body`)}</p>
                <pre className="mt-5 max-w-full overflow-x-auto rounded-md bg-white/[0.04] p-4 text-xs leading-relaxed text-accent-light"><code>{t(`migration.playbooks.${playbook}.code`, { apiBaseUrl })}</code></pre>
              </article>
            ))}
          </div>
        </section>

        <aside className="border-t border-line pt-12">
          <p className="font-mono text-label tracking-[0.16em] text-warn uppercase">{t('contract.eyebrow')}</p>
          <div className="mt-4 grid gap-5 lg:grid-cols-[0.8fr_1.2fr]">
            <h2 className="text-3xl">{t('contract.title')}</h2>
            <p className="text-ui leading-relaxed text-muted">{t('contract.body')}</p>
          </div>
        </aside>
      </div>
    </div>
  );
}
