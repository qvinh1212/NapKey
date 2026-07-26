import { getTranslations, setRequestLocale } from 'next-intl/server';
import type { Metadata } from 'next';
import type { Locale } from '@/i18n/routing';
import { WalletTopup } from '@/components/console/wallet-topup';

export async function generateMetadata({ params }: { params: Promise<{ locale: string }> }): Promise<Metadata> {
  const { locale } = await params;
  const t = await getTranslations({ locale, namespace: 'console.wallet' });
  return { title: t('metaTitle'), robots: { index: false, follow: false } };
}

export default async function WalletPage({ params }: { params: Promise<{ locale: string }> }) {
  const { locale } = await params;
  setRequestLocale(locale as Locale);
  return <WalletTopup />;
}
