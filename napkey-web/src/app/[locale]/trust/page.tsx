import type { Metadata } from 'next';
import { getTranslations, setRequestLocale } from 'next-intl/server';
import type { Locale } from '@/i18n/routing';
import { Link } from '@/i18n/navigation';
import { DocumentSection, PublicDocument } from '@/components/napkey/public-document';
import { publicPageMetadata } from '@/lib/public-metadata';

const sections = ['identity', 'keyHandling', 'requestData', 'billing', 'upstream', 'limits'] as const;

export async function generateMetadata({ params }: { params: Promise<{ locale: string }> }): Promise<Metadata> {
  const { locale } = await params;
  const t = await getTranslations({ locale, namespace: 'trustPage' });
  return publicPageMetadata(locale, 'trust', t('metaTitle'), t('intro'));
}

export default async function TrustPage({ params }: { params: Promise<{ locale: string }> }) {
  const { locale } = await params;
  setRequestLocale(locale as Locale);
  const t = await getTranslations('trustPage');

  return (
    <PublicDocument eyebrow={t('eyebrow')} title={t('title')} intro={t('intro')}>
      <div className="mb-6 flex flex-wrap gap-3">
        <Link href="/status" className="rounded-full bg-fg px-5 py-2.5 text-ui font-medium text-bg">{t('statusLink')}</Link>
        <Link href="/privacy" className="rounded-full border border-line px-5 py-2.5 text-ui text-muted hover:text-fg">{t('privacyLink')}</Link>
      </div>
      {sections.map((key, index) => (
        <DocumentSection key={key} index={String(index + 1).padStart(2, '0')} title={t(`sections.${key}.title`)}>
          <p>{t(`sections.${key}.body`)}</p>
        </DocumentSection>
      ))}
    </PublicDocument>
  );
}
