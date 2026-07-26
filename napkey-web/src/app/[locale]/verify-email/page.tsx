import { getTranslations, setRequestLocale } from 'next-intl/server';
import type { Metadata } from 'next';
import type { Locale } from '@/i18n/routing';
import { SessionProvider } from '@/components/console/session-provider';
import { VerifyEmail } from '@/components/console/verify-email';
import { AuthShell } from '@/components/console/auth-shell';

export async function generateMetadata({
  params,
}: {
  params: Promise<{ locale: string }>;
}): Promise<Metadata> {
  const { locale } = await params;
  const t = await getTranslations({ locale, namespace: 'console.verify' });
  return { title: t('metaTitle'), robots: { index: false, follow: false } };
}

/**
 * Duong dan nay khop voi link trong email cua napkey-core (`mail/templates.go`).
 * Doi ten trang la lam moi email da gui truoc do thanh link chet.
 */
export default async function VerifyEmailPage({
  params,
  searchParams,
}: {
  params: Promise<{ locale: string }>;
  searchParams: Promise<{ token?: string }>;
}) {
  const { locale } = await params;
  const { token } = await searchParams;
  setRequestLocale(locale as Locale);

  return (
    <AuthShell>
      <SessionProvider>
        <VerifyEmail token={token ?? null} />
      </SessionProvider>
    </AuthShell>
  );
}