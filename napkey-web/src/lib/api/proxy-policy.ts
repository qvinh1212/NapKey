// Keep this list exact: the BFF is the public boundary around napkey-core.
const ALLOWED: readonly (RegExp | string)[] = [
  '/v1/auth/register',
  '/v1/auth/login',
  '/v1/auth/google/start',
  '/v1/auth/google/callback',
  '/v1/auth/logout',
  '/v1/auth/verify-email',
  '/v1/auth/resend-verification',
  '/v1/auth/forgot-password',
  '/v1/auth/reset-password',
  '/v1/auth/session',
  '/v1/status',
  '/v1/me/password',
  '/v1/me/usage',
  '/v1/me/usage/detail',
  '/v1/me/usage/records',
  '/v1/me/wallet',
  '/v1/me/topups',
  /^\/v1\/me\/topups\/[A-Za-z0-9-]{1,64}$/,
  '/v1/keys',
  /^\/v1\/keys\/[A-Za-z0-9-]{1,64}$/,
  '/v1/admin/operations/summary',
  '/v1/admin/business/summary',
  '/v1/admin/operations/alerts',
  '/v1/admin/operations/reconcile-wallets',
  /^\/v1\/admin\/users\/[A-Za-z0-9-]{1,64}\/roles$/,
  // PayOS signs the JSON body, so this route is public without session auth.
  '/webhooks/payos',
];

export function isProxyPathAllowed(pathname: string): boolean {
  return ALLOWED.some((rule) =>
    typeof rule === 'string' ? rule === pathname : rule.test(pathname),
  );
}
