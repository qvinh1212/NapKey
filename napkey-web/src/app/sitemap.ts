import type { MetadataRoute } from 'next';
import { locales, routing } from '@/i18n/routing';
import { site } from '@/lib/site';

export default function sitemap(): MetadataRoute.Sitemap {
  const paths = ['', '/compatibility', '/trust', '/status', '/privacy', '/terms'] as const;

  return locales.flatMap((locale) => paths.map((path) => ({
    url: `${site.url}/${locale}${path}`,
    changeFrequency: 'weekly',
    priority: path === '' ? (locale === 'vi' ? 1 : 0.8) : 0.5,
    alternates: {
      languages: {
        ...Object.fromEntries(locales.map((l) => [l, `${site.url}/${l}${path}`])),
        'x-default': `${site.url}/${routing.defaultLocale}${path}`,
      },
    },
  })));
}
