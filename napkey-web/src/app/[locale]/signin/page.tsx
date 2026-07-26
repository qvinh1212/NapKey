import { getTranslations, setRequestLocale } from 'next-intl/server';
import type { Metadata } from 'next';
import type { Locale } from '@/i18n/routing';
import { SessionProvider } from '@/components/console/session-provider';
import { AuthForm } from '@/components/console/auth-form';
import { AuthShell } from '@/components/console/auth-shell';

export async function generateMetadata({
  params,
}: {
  params: Promise<{ locale: string }>;
}): Promise<Metadata> {
  const { locale } = await params;
  const t = await getTranslations({ locale, namespace: 'console.auth' });
  return { title: t('signin.title'), robots: { index: false, follow: false } };
}

export default async function SignInPage({ params }: { params: Promise<{ locale: string }> }) {
  const { locale } = await params;
  setRequestLocale(locale as Locale);

  return (
    <AuthShell>
      <SessionProvider>
        <AuthForm mode="signin" />
      </SessionProvider>
    </AuthShell>
  );
}