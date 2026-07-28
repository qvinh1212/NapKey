import type { Metadata } from 'next';
import { getTranslations, setRequestLocale } from 'next-intl/server';
import { DeveloperWorkbench } from '@/components/console/developer-workbench';
import type { Locale } from '@/i18n/routing';
import { readModelCatalog } from '@/lib/model-catalog';
import { site } from '@/lib/site';

export async function generateMetadata({ params }: { params: Promise<{ locale: string }> }): Promise<Metadata> {
  const { locale } = await params;
  const t = await getTranslations({ locale, namespace: 'console.developer' });
  return { title: t('metaTitle'), robots: { index: false, follow: false } };
}

export default async function DeveloperPage({ params }: { params: Promise<{ locale: string }> }) {
  const { locale } = await params;
  setRequestLocale(locale as Locale);
  const catalog = await readModelCatalog();
  return <DeveloperWorkbench catalog={catalog} apiBaseUrl={site.apiBaseUrl.replace(/\/+$/, '')} />;
}
