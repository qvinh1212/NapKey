import type { Metadata } from 'next';
import { getTranslations, setRequestLocale } from 'next-intl/server';
import type { Locale } from '@/i18n/routing';
import { PublicDocument } from '@/components/napkey/public-document';
import { compactUptime, readPublicStatus } from '@/lib/public-status';
import { site } from '@/lib/site';
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
  const checkedAt = new Intl.DateTimeFormat(locale, {
    year: 'numeric', month: 'short', day: 'numeric',
    hour: '2-digit', minute: '2-digit', second: '2-digit',
    timeZone: 'Asia/Ho_Chi_Minh', timeZoneName: 'short',
  }).format(new Date());

  return (
    <PublicDocument eyebrow={t('eyebrow')} title={t('title')} intro={t('intro')}>
      <section className={`rounded-lg border p-6 sm:p-8 ${status.operational ? 'border-accent/35 bg-accent-soft' : 'border-danger/35 bg-danger-soft'}`}>
        <div className="flex flex-wrap items-start justify-between gap-6">
          <div>
            <p className={`font-mono text-label tracking-[0.14em] uppercase ${status.operational ? 'text-accent-light' : 'text-danger'}`}>
              {status.operational ? t('reachable') : t('unavailable')}
            </p>
            <h2 className="mt-3 text-3xl tracking-[-0.02em]">{t('gatewayProcess')}</h2>
            <p className="mt-2 text-ui text-muted">{t(status.operational ? 'reachableBody' : 'unavailableBody')}</p>
          </div>
          <span aria-hidden className={`size-4 rounded-full ${status.operational ? 'bg-accent shadow-[0_0_24px_rgba(16,185,129,0.8)]' : 'bg-danger'}`} />
        </div>
        <dl className="mt-8 grid gap-px overflow-hidden rounded-md border border-line bg-line sm:grid-cols-3">
          <div className="bg-black/60 p-4"><dt className="font-mono text-label text-dim uppercase">{t('endpoint')}</dt><dd className="mt-2 break-all font-mono text-ui text-muted">{site.apiBaseUrl}</dd></div>
          <div className="bg-black/60 p-4"><dt className="font-mono text-label text-dim uppercase">{t('version')}</dt><dd className="mt-2 font-mono text-ui text-muted">{status.version || '—'}</dd></div>
          <div className="bg-black/60 p-4"><dt className="font-mono text-label text-dim uppercase">{t('uptime')}</dt><dd className="mt-2 font-mono text-ui text-muted">{status.operational ? compactUptime(status.uptimeSeconds) : '—'}</dd></div>
        </dl>
        <div className="mt-5 flex flex-wrap justify-between gap-2 text-ui text-dim"><p>{t('scope')}</p><p>{t('checkedAt', { time: checkedAt })}</p></div>
      </section>
    </PublicDocument>
  );
}
