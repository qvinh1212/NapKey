import type { Metadata } from 'next';
import type { ReactNode } from 'react';
import { Inter, Manrope } from 'next/font/google';
import { NextIntlClientProvider, hasLocale } from 'next-intl';
import { getTranslations, setRequestLocale } from 'next-intl/server';
import { notFound } from 'next/navigation';
import { routing, locales, type Locale } from '@/i18n/routing';
import { site } from '@/lib/site';
import { SiteHeader } from '@/components/napkey/site-header';
import { SiteFooter } from '@/components/napkey/site-footer';
import '../globals.css';

const manrope = Manrope({
  subsets: ['latin', 'vietnamese'],
  weight: ['600', '700'],
  variable: '--font-manrope',
  display: 'swap',
});

const inter = Inter({
  subsets: ['latin', 'vietnamese'],
  variable: '--font-inter',
  display: 'swap',
});

export function generateStaticParams() {
  return locales.map((locale) => ({ locale }));
}

export async function generateMetadata({
  params,
}: {
  params: Promise<{ locale: string }>;
}): Promise<Metadata> {
  const { locale } = await params;
  const t = await getTranslations({ locale, namespace: 'meta' });

  // hreflang cho tung trang, neu khong hai ban dich se tu canh tranh
  // nhau tren ket qua tim kiem.
  const languages = Object.fromEntries(locales.map((code) => [code, `/${code}`]));

  return {
    metadataBase: new URL(site.url),
    title: t('title'),
    description: t('description'),
    alternates: {
      canonical: `/${locale}`,
      languages: { ...languages, 'x-default': `/${routing.defaultLocale}` },
    },
    openGraph: {
      type: 'website',
      siteName: site.name,
      locale: locale === 'vi' ? 'vi_VN' : 'en_US',
      url: `/${locale}`,
      title: t('title'),
      description: t('description'),
    },
    twitter: { card: 'summary_large_image', title: t('title'), description: t('description') },
    robots: { index: true, follow: true },
  };
}

export default async function LocaleLayout({
  children,
  params,
}: {
  children: ReactNode;
  params: Promise<{ locale: string }>;
}) {
  const { locale } = await params;
  if (!hasLocale(routing.locales, locale)) notFound();

  setRequestLocale(locale as Locale);
  const t = await getTranslations({ locale, namespace: 'nav' });

  return (
    <html lang={locale} className={`${manrope.variable} ${inter.variable}`} suppressHydrationWarning>
      <body className="min-h-dvh antialiased">
        <NextIntlClientProvider>
          <a
            href="#main"
            className="sr-only focus:not-sr-only focus:fixed focus:top-4 focus:left-4 focus:z-100 focus:rounded-full focus:bg-fg focus:px-5 focus:py-2.5 focus:text-ui focus:text-bg"
          >
            {t('skipToContent')}
          </a>
          <SiteHeader />
          <main id="main">{children}</main>
          <SiteFooter />
        </NextIntlClientProvider>
      </body>
    </html>
  );
}
