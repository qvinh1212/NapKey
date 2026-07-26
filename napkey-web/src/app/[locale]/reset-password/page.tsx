import { getTranslations, setRequestLocale } from 'next-intl/server';
import type { Metadata } from 'next';
import type { Locale } from '@/i18n/routing';
import { SessionProvider } from '@/components/console/session-provider';
import { ResetPassword } from '@/components/console/reset-password';
import { AuthShell } from '@/components/console/auth-shell';

export async function generateMetadata({
  params,
}: {
  params: Promise<{ locale: string }>;
}): Promise<Metadata> {
  const { locale } = await params;
  const t = await getTranslations({ locale, namespace: 'console.reset' });
  return { title: t('metaTitle'), robots: { index: false, follow: false } };
}

export default async function ResetPasswordPage({
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
        <ResetPassword token={token ?? null} />
      </SessionProvider>
    </AuthShell>
  );
}