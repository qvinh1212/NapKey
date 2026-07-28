'use client';

import { useEffect, useState } from 'react';
import { useLocale, useTranslations } from 'next-intl';
import { Link, usePathname, useRouter } from '@/i18n/navigation';
import { api } from '@/lib/api/client';
import { useSession } from './session-provider';
import { Badge } from './ui';

/**
 * Vo bao quanh moi trang console: kiem phien, sidebar, banner canh bao.
 *
 * Viec chuyen huong nam o day chu khong o middleware, vi cookie phien la HttpOnly
 * va chi napkey-core xac thuc duoc. Xem ghi chu trong `session-provider.tsx`.
 *
 * Sidebar doc theo DESIGN.md muc 8. Tren mobile no thanh mot hang tab ngang - mot
 * sidebar 240px tren man hinh 375px thi khong con cho cho bang so lieu.
 */

const tabs = [
  { key: 'overview', href: '/console' },
  { key: 'usage', href: '/console/usage' },
  { key: 'wallet', href: '/console/billing' },
  { key: 'keys', href: '/console/keys' },
  { key: 'developer', href: '/console/developer' },
  { key: 'settings', href: '/console/settings' },
] as const;

/** Nut gui lai email xac minh, dat trong banner canh bao. */
function ResendVerification({ email }: { email: string }) {
  const t = useTranslations('console.shell');
  const locale = useLocale();
  const [state, setState] = useState<'idle' | 'sending' | 'sent' | 'failed'>('idle');

  async function resend() {
    setState('sending');
    try {
      await api.post('/v1/auth/resend-verification', { email, locale });
      setState('sent');
    } catch {
      setState('failed');
    }
  }

  if (state === 'sent') {
    return (
      <span role="status" className="text-ui text-accent-light">
        {t('resendSent')}
      </span>
    );
  }

  return (
    <button
      type="button"
      onClick={() => void resend()}
      disabled={state === 'sending'}
      className="rounded-full border border-warn/40 px-4 py-1.5 text-ui text-warn transition-colors hover:bg-warn/10 disabled:pointer-events-none disabled:opacity-50"
    >
      {state === 'sending' ? t('resendSending') : state === 'failed' ? t('resendRetry') : t('resend')}
    </button>
  );
}

export function ConsoleShell({ children }: { children: React.ReactNode }) {
  const t = useTranslations('console.shell');
  const session = useSession();
  const router = useRouter();
  const pathname = usePathname();

  // Khach chua dang nhap thi dua ve trang dang nhap. Chay trong effect vi dieu
  // huong trong khi render la loi trong React.
  useEffect(() => {
    if (session.status === 'anonymous') router.replace('/signin');
  }, [session.status, router]);

  if (session.status === 'loading') {
    return (
      <div className="container-page flex min-h-[60vh] items-center justify-center">
        <p role="status" className="text-ui text-dim">
          {t('checkingSession')}
        </p>
      </div>
    );
  }

  if (session.status === 'anonymous') {
    // Man hinh trung gian trong khi dieu huong dien ra. Khong render children de
    // tranh mot loat request 401 vo nghia.
    return (
      <div className="container-page flex min-h-[60vh] items-center justify-center">
        <p role="status" className="text-ui text-dim">
          {t('redirectingToSignIn')}
        </p>
      </div>
    );
  }

  const { user } = session;

  return (
    <div className="container-page pt-28 pb-24">
      <div className="flex flex-col gap-8 lg:flex-row lg:gap-10">
        <div className="lg:w-52 lg:shrink-0">
          <div className="mb-6">
            <p className="font-mono text-label tracking-[0.18em] text-accent uppercase">
              {t('eyebrow')}
            </p>
            <h1 className="mt-2 text-2xl tracking-[-0.02em]">{t('title')}</h1>
          </div>

          {/*
            Tren mobile: hang ngang cuon duoc. Tren desktop: cot doc.
            `-mx-6 px-6` de vung cuon tran ra sat le man hinh, tranh cam giac bi cat.
          */}
          <nav
            aria-label={t('navLabel')}
            className="-mx-6 mb-4 flex gap-1 overflow-x-auto border-b border-line px-6 lg:mx-0 lg:mb-0 lg:flex-col lg:overflow-visible lg:border-b-0 lg:px-0"
          >
            {[...tabs, ...(session.permissions.includes('operations.read') ? [{ key: 'operations' as const, href: '/console/admin' as const }] : [])].map(({ key, href }) => {
              // So sanh chinh xac cho tab goc, tien to cho tab con: `/console` khong
              // duoc sang khi dang o `/console/usage`.
              const isActive = href === '/console' ? pathname === href : pathname.startsWith(href);
              return (
                <Link
                  key={key}
                  href={href}
                  aria-current={isActive ? 'page' : undefined}
                  className={
                    'shrink-0 whitespace-nowrap text-ui transition-colors duration-150 ease-[var(--ease-smooth)] ' +
                    '-mb-px border-b-2 px-4 py-2.5 ' +
                    'lg:mb-0 lg:rounded-md lg:border-b-0 lg:border-l-2 lg:px-3 lg:py-2 ' +
                    (isActive
                      ? 'border-accent text-fg lg:bg-surface'
                      : 'border-transparent text-dim hover:text-muted lg:hover:bg-surface')
                  }
                >
                  {key === 'operations' ? 'Vận hành' : t(`tabs.${key}`)}
                </Link>
              );
            })}
          </nav>

          <div className="hidden lg:mt-8 lg:block">
            <p className="truncate text-ui text-dim" title={user.email}>
              {user.email}
            </p>
            <button
              type="button"
              onClick={() => void session.signOut()}
              className="mt-2 rounded-full border border-line bg-surface-hover px-4 py-1.5 text-ui text-muted transition-colors hover:bg-white/10 hover:text-fg"
            >
              {t('signOut')}
            </button>
          </div>
        </div>

        <div className="min-w-0 flex-1">
          {/* Tren mobile phan tai khoan nam tren dau noi dung, vi sidebar da thu gon. */}
          <div className="mb-6 flex items-center justify-between gap-3 lg:hidden">
            <span className="min-w-0 truncate text-ui text-dim" title={user.email}>
              {user.email}
            </span>
            <button
              type="button"
              onClick={() => void session.signOut()}
              className="shrink-0 rounded-full border border-line bg-surface-hover px-4 py-1.5 text-ui text-muted transition-colors hover:bg-white/10 hover:text-fg"
            >
              {t('signOut')}
            </button>
          </div>

          {/*
            Chua xac minh email thi khong tao duoc key (napkey-core chan o requireVerified).
            Noi ro o day kem nut gui lai, neu khong khach se bam Tao key va nhan mot loi
            khong giai thich gi.
          */}
          {!user.emailVerified ? (
            <div
              role="status"
              className="mb-6 flex flex-wrap items-center gap-3 rounded-lg border border-warn/30 bg-warn/10 px-5 py-4"
            >
              <Badge tone="warn">{t('unverifiedBadge')}</Badge>
              <p className="min-w-0 flex-1 text-ui text-warn">{t('unverifiedMessage')}</p>
              <ResendVerification email={user.email} />
            </div>
          ) : null}

          {children}
        </div>
      </div>
    </div>
  );
}
