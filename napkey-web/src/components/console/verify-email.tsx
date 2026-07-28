'use client';

import { useEffect, useState } from 'react';
import { useLocale, useTranslations } from 'next-intl';
import { Link } from '@/i18n/navigation';
import { api, ApiError } from '@/lib/api/client';
import { Panel } from './ui';

/**
 * Trang nhan link xac minh email.
 *
 * napkey-core gui email tro toi `/{locale}/verify-email?token=...` (xem
 * `mail/templates.go`), nen duong dan nay la mot phan cua hop dong voi backend -
 * doi ten no la lam moi email da gui truoc do thanh link chet.
 *
 * Token duoc doi ngay khi trang mo, khong cho nguoi dung bam them mot nut: ho da
 * bam mot lan trong email roi, bat bam lan nua chi them mot buoc vo nghia.
 */

type State =
  | { status: 'verifying' }
  | { status: 'done'; trialGranted: boolean }
  | { status: 'failed'; message: string; expired: boolean };

export function VerifyEmail({ token }: { token: string | null }) {
  const t = useTranslations('console.verify');
  const locale = useLocale();
  const [state, setState] = useState<State>(
    // Khong co token thi khong can goi mang. Xay ra khi nguoi dung mo tay duong dan.
    token ? { status: 'verifying' } : { status: 'failed', message: '', expired: false },
  );

  useEffect(() => {
    if (!token) return;
    const controller = new AbortController();

    async function run() {
      try {
        const response = await api.post<{ trial?: { granted?: boolean } }>('/v1/auth/verify-email', { token });
        if (!controller.signal.aborted) setState({ status: 'done', trialGranted: response.trial?.granted === true });
      } catch (error) {
        if (controller.signal.aborted) return;
        // Backend gop het het-han, da-dung, khong-ton-tai vao mot loi 400 de khong
        // ai do duoc token nao la that. Nen o day cung chi noi mot cau.
        const expired = error instanceof ApiError && error.status === 400;
        const message = error instanceof ApiError ? error.message : t('networkError');
        setState({ status: 'failed', message, expired });
      }
    }

    void run();
    return () => controller.abort();
  }, [token, t]);

  if (state.status === 'verifying') {
    return (
      <Panel className="p-8 text-center">
        <p role="status" className="text-ui text-dim">
          {t('verifying')}
        </p>
      </Panel>
    );
  }

  if (state.status === 'done') {
    return (
      <Panel className="p-8 text-center">
        <p className="font-mono text-label tracking-[0.18em] text-accent uppercase">
          {t('successEyebrow')}
        </p>
        <h1 className="mt-3 text-xl tracking-[-0.02em]">{t('successTitle')}</h1>
        <p className="mx-auto mt-3 max-w-sm text-ui leading-relaxed text-muted">
          {t('successBody')}
        </p>
        <p className="mx-auto mt-3 max-w-sm rounded-md border border-line bg-surface-hover px-4 py-3 text-ui leading-relaxed text-muted">
          {state.trialGranted ? t('trialGranted') : t('trialUnavailable')}
        </p>
        <Link
          href="/console"
          className="mt-6 inline-flex rounded-full bg-fg px-6 py-2.5 text-ui font-medium text-bg transition-colors hover:bg-white/90"
        >
          {t('goToConsole')}
        </Link>
      </Panel>
    );
  }

  return (
    <Panel className="p-8 text-center">
      <h1 className="text-xl tracking-[-0.02em]">{t('failedTitle')}</h1>
      <p className="mx-auto mt-3 max-w-sm text-ui leading-relaxed text-muted">
        {token ? state.message || t('failedBody') : t('missingToken')}
      </p>
      {/*
        Link het han la truong hop thuong gap nhat (song 24 gio), nen loi ra duoc
        mot duong di tiep thay vi de nguoi dung mac ket.
      */}
      <div className="mt-6 flex flex-wrap justify-center gap-3">
        <Link
          href="/resend-verification"
          className="rounded-full bg-fg px-5 py-2.5 text-ui font-medium text-bg transition-colors hover:bg-white/90"
        >
          {t('resendLink')}
        </Link>
        <Link
          href="/signin"
          className="rounded-full border border-line px-5 py-2.5 text-ui text-muted transition-colors hover:bg-white/10 hover:text-fg"
        >
          {t('backToSignIn')}
        </Link>
      </div>
      <p className="mt-4 text-ui text-dim" lang={locale}>
        {t('failedHint')}
      </p>
    </Panel>
  );
}
