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

export default async function SignInPage({
  params,
  searchParams,
}: {
  params: Promise<{ locale: string }>;
  searchParams: Promise<{ oauth_error?: string }>;
}) {
  const { locale } = await params;
  setRequestLocale(locale as Locale);
  // Doc tren server thay vi useSearchParams: tranh bat toan bo form vao Suspense chi
  // de doc mot query param.
  const { oauth_error: oauthError } = await searchParams;

  return (
    <AuthShell>
      <SessionProvider>
        <AuthForm mode="signin" oauthError={oauthError} />
      </SessionProvider>
    </AuthShell>
  );
}