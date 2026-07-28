import type { Metadata } from 'next';
import { getTranslations, setRequestLocale } from 'next-intl/server';
import type { Locale } from '@/i18n/routing';
import { DocumentSection, PublicDocument } from '@/components/napkey/public-document';
import { publicPageMetadata } from '@/lib/public-metadata';

const sections = ['account', 'prompts', 'usage', 'payments', 'cookies', 'choices'] as const;

export async function generateMetadata({ params }: { params: Promise<{ locale: string }> }): Promise<Metadata> {
  const { locale } = await params;
  const t = await getTranslations({ locale, namespace: 'privacyPage' });
  return publicPageMetadata(locale, 'privacy', t('metaTitle'), t('intro'));
}

export default async function PrivacyPage({ params }: { params: Promise<{ locale: string }> }) {
  const { locale } = await params;
  setRequestLocale(locale as Locale);
  const t = await getTranslations('privacyPage');
  return <PublicDocument eyebrow={t('eyebrow')} title={t('title')} intro={t('intro')}>{sections.map((key, index) => <DocumentSection key={key} index={String(index + 1).padStart(2, '0')} title={t(`sections.${key}.title`)}><p>{t(`sections.${key}.body`)}</p></DocumentSection>)}</PublicDocument>;
}
