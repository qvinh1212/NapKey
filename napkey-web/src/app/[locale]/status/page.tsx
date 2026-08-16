import type { Metadata } from 'next';
import { getTranslations, setRequestLocale } from 'next-intl/server';
import type { Locale } from '@/i18n/routing';
import { PublicDocument } from '@/components/napkey/public-document';
import { readPublicStatus } from '@/lib/public-status';
import { publicPageMetadata } from '@/lib/public-metadata';

export const dynamic = 'force-dynamic';

export async function generateMetadata({ params }: { params: Promise<{ locale: string }> }): Promise<Metadata> {
  const { locale } = await params;
  const t = await getTranslations({ locale, namespace: 'statusPage' });
  return publicPageMetadata(locale, 'status', t('metaTitle'), t('intro'));
}

export default async function StatusPage({ params }: { params: Promise<{ locale: string }> }) {
  const { locale } = await params;
  setRequestLocale(locale as Locale);
  const [t, status] = await Promise.all([getTranslations('statusPage'), readPublicStatus()]);
  const checkedAt = status.checkedAt ? new Intl.DateTimeFormat(locale, {
    year: 'numeric', month: 'short', day: 'numeric',
    hour: '2-digit', minute: '2-digit', second: '2-digit',
    timeZone: 'Asia/Ho_Chi_Minh', timeZoneName: 'short',
  }).format(new Date(status.checkedAt)) : t('unknownTime');

  return (
    <PublicDocument eyebrow={t('eyebrow')} title={t('title')} intro={t('intro')}>
      <section className={`rounded-xl border p-6 sm:p-8 ${status.status === 'operational' ? 'border-accent/35 bg-accent-soft' : status.status === 'degraded' ? 'border-warn/35 bg-warn/5' : 'border-danger/35 bg-danger-soft'}`}>
        <div className="flex flex-wrap items-start justify-between gap-6">
          <div>
            <p className={`font-mono text-label tracking-[0.14em] uppercase ${status.status === 'operational' ? 'text-accent-light' : status.status === 'degraded' ? 'text-warn' : 'text-danger'}`}>
              {t(`states.${status.status}`)}
            </p>
            <h2 className="mt-3 text-3xl tracking-[-0.02em]">{t('serviceTitle')}</h2>
            <p className="mt-2 text-ui text-muted">{t(`stateBodies.${status.status}`)}</p>
          </div>
          <span aria-hidden className={`size-2.5 rounded-full ${status.status === 'operational' ? 'bg-success shadow-[0_0_8px_rgba(52,211,153,0.55)]' : status.status === 'degraded' ? 'bg-warn' : 'bg-danger'}`} />
        </div>
        <dl className="mt-8 grid gap-px overflow-hidden rounded-lg border border-line bg-line sm:grid-cols-3">
          {status.components.map((component) => (
            <div key={component.id} className="bg-surface-3 p-4">
              <dt className="font-mono text-label text-dim uppercase">{t(`components.${component.id}`)}</dt>
              <dd className={`mt-2 font-mono text-ui ${component.status === 'operational' ? 'text-success' : component.status === 'degraded' ? 'text-warn' : 'text-danger'}`}>{t(`states.${component.status}`)}</dd>
            </div>
          ))}
        </dl>
        <div className="mt-5 flex flex-wrap justify-between gap-2 text-ui text-dim"><p>{t('scope')}</p><p>{t('checkedAt', { time: checkedAt })}</p></div>
      </section>
    </PublicDocument>
  );
}
