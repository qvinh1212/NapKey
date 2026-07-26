export const site = {
  name: 'NapKey',
  url: process.env.NEXT_PUBLIC_SITE_URL ?? 'https://napkey.vn',
  apiBaseUrl: process.env.NEXT_PUBLIC_API_BASE_URL ?? 'https://api.napkey.vn',
  /** Toi thieu moi lan nap, don vi dong. Khop voi DESIGN.md muc 6.1. */
  minTopUpVnd: 20_000,
} as const;
