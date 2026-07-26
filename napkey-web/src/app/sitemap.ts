import type { MetadataRoute } from 'next';
import { locales } from '@/i18n/routing';
import { site } from '@/lib/site';

export default function sitemap(): MetadataRoute.Sitemap {
  const now = new Date();

  return locales.map((locale) => ({
    url: `${site.url}/${locale}`,
    lastModified: now,
    changeFrequency: 'weekly',
    priority: locale === 'vi' ? 1 : 0.8,
    alternates: {
      languages: Object.fromEntries(locales.map((l) => [l, `${site.url}/${l}`])),
    },
  }));
}
