export function googleAuthPath(locale: string): string {
  const safeLocale = locale === 'en' ? 'en' : 'vi';
  return `/api/v1/auth/google/start?locale=${safeLocale}`;
}

/**
 * Ma loi tu napkey-core sang khoa message.
 *
 * `?oauth_error=` den tu URL nen khong duoc hien thi truc tiep: mot ma la se thanh
 * dong chu do khach tu dat tren trang dang nhap. Chi nhung ma trong bang nay co cau
 * tra loi rieng, con lai roi ve `generic`.
 */
const OAUTH_ERROR_CODES = [
  'expired',
  'invalid_state',
  'cancelled',
  'provider',
  'profile',
  'account_conflict',
  'suspended',
  'unconfigured',
  'rate_limited',
] as const;

type KnownOAuthError = (typeof OAUTH_ERROR_CODES)[number];

export type GoogleErrorKey = KnownOAuthError | 'generic';

export function googleErrorKey(code: string | null | undefined): GoogleErrorKey | null {
  if (!code) return null;
  return OAUTH_ERROR_CODES.includes(code as KnownOAuthError) ? (code as GoogleErrorKey) : 'generic';
}
