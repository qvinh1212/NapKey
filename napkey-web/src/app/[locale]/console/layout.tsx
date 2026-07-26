import type { Metadata } from 'next';
import type { ReactNode } from 'react';
import { setRequestLocale } from 'next-intl/server';
import type { Locale } from '@/i18n/routing';
import { SessionProvider } from '@/components/console/session-provider';
import { ConsoleShell } from '@/components/console/console-shell';

/**
 * Console khong bao gio duoc index: no chi chua du lieu rieng cua tung nguoi va
 * khong co gia tri gi tren ket qua tim kiem.
 */
export const metadata: Metadata = {
  robots: { index: false, follow: false },
};

export default async function ConsoleLayout({
  children,
  params,
}: {
  children: ReactNode;
  params: Promise<{ locale: string }>;
}) {
  const { locale } = await params;
  setRequestLocale(locale as Locale);

  return (
    <SessionProvider>
      <ConsoleShell>{children}</ConsoleShell>
    </SessionProvider>
  );
}