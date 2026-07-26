import { defineRouting } from 'next-intl/routing';

export const locales = ['vi', 'en'] as const;
export type Locale = (typeof locales)[number];

export const defaultLocale: Locale = 'vi';

export const routing = defineRouting({
  locales,
  defaultLocale,
  localePrefix: 'always',
  localeCookie: {
    name: 'NAPKEY_LOCALE',
    maxAge: 60 * 60 * 24 * 365,
    sameSite: 'lax',
  },
});
