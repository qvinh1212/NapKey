import type { Metadata } from 'next';
import { locales, routing } from '@/i18n/routing';
import { site } from '@/lib/site';

export function publicPageMetadata(locale: string, path: string, title: string, description: string): Metadata {
  const localizedPath = `/${locale}/${path}`;
  const languages = Object.fromEntries(locales.map((code) => [code, `/${code}/${path}`]));

  return {
    title,
    description,
    alternates: {
      canonical: localizedPath,
      languages: { ...languages, 'x-default': `/${routing.defaultLocale}/${path}` },
    },
    openGraph: {
      type: 'website',
      siteName: site.name,
      locale: locale === 'vi' ? 'vi_VN' : 'en_US',
      url: localizedPath,
      title,
      description,
    },
    twitter: { card: 'summary_large_image', title, description },
  };
}
