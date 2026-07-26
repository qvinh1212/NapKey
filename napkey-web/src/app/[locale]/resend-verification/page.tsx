import { getTranslations, setRequestLocale } from 'next-intl/server';
import type { Metadata } from 'next';
import type { Locale } from '@/i18n/routing';
import { SessionProvider } from '@/components/console/session-provider';
import { EmailRequestForm } from '@/components/console/email-request-form';
import { AuthShell } from '@/components/console/auth-shell';

export async function generateMetadata({
  params,
}: {
  params: Promise<{ locale: string }>;
}): Promise<Metadata> {
  const { locale } = await params;
  const t = await getTranslations({ locale, namespace: 'console.resend' });
  return { title: t('metaTitle'), robots: { index: false, follow: false } };
}

export default async function ResendVerificationPage({
  params,
}: {
  params: Promise<{ locale: string }>;
}) {
  const { locale } = await params;
  setRequestLocale(locale as Locale);

  return (
    <AuthShell>
      <SessionProvider>
        <EmailRequestForm kind="resend" />
      </SessionProvider>
    </AuthShell>
  );
}