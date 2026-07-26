'use client';

import { useState } from 'react';
import { useLocale, useTranslations } from 'next-intl';
import { Link } from '@/i18n/navigation';
import { api, ApiError } from '@/lib/api/client';
import { Field } from './field';
import { Panel } from './ui';

/**
 * Form gui lai mot email: quen mat khau, hoac gui lai link xac minh.
 *
 * Hai luong dung chung mot component vi chung giong nhau den tan hanh vi: nhap
 * email, backend tra 202, va man hinh ket qua KHONG noi email do co ton tai hay
 * khong.
 *
 * Diem quan trong: backend luon tra 202 du email co trong he thong hay khong (xem
 * `handleForgotPassword`). Neu UI hien "khong tim thay email nay" thi da bien mot
 * endpoint cong khai thanh cong cu do xem ai la khach hang. Nen man hinh thanh cong
 * o day co y dung mot cau chung chung.
 *
 * `locale` duoc gui kem de email den dung ngon ngu nguoi dung dang xem.
 */

type Kind = 'forgot' | 'resend';

const endpoints: Record<Kind, string> = {
  forgot: '/v1/auth/forgot-password',
  resend: '/v1/auth/resend-verification',
};

export function EmailRequestForm({ kind }: { kind: Kind }) {
  const t = useTranslations(`console.${kind}`);
  const locale = useLocale();

  const [email, setEmail] = useState('');
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [fieldErrors, setFieldErrors] = useState<Record<string, string>>({});
  const [sent, setSent] = useState(false);

  async function submit(event: React.FormEvent) {
    event.preventDefault();
    setPending(true);
    setError(null);
    setFieldErrors({});
    try {
      await api.post(endpoints[kind], { email: email.trim(), locale });
      setSent(true);
    } catch (err) {
      if (err instanceof ApiError) {
        if (err.fields) setFieldErrors(err.fields);
        // Rate limit la loi duy nhat dang noi ro: nguoi dung can biet la phai cho.
        setError(err.message);
      } else {
        setError(t('networkError'));
      }
    } finally {
      setPending(false);
    }
  }

  if (sent) {
    return (
      <Panel className="p-8 text-center">
        <h1 className="text-xl tracking-[-0.02em]">{t('sentTitle')}</h1>
        <p className="mx-auto mt-3 max-w-sm text-ui leading-relaxed text-muted">
          {t('sentBody', { email: email.trim() })}
        </p>
        <Link
          href="/signin"
          className="mt-6 inline-flex rounded-full border border-line px-5 py-2.5 text-ui text-muted transition-colors hover:bg-white/10 hover:text-fg"
        >
          {t('backToSignIn')}
        </Link>
      </Panel>
    );
  }

  return (
    <Panel className="p-8">
      <h1 className="text-xl tracking-[-0.02em]">{t('title')}</h1>
      <p className="mt-2 text-ui leading-relaxed text-dim">{t('subtitle')}</p>

      <form onSubmit={submit} className="mt-6 flex flex-col gap-4" noValidate>
        <Field
          id="request-email"
          type="email"
          required
          autoComplete="email"
          label={t('emailLabel')}
          error={fieldErrors.email}
          value={email}
          onChange={(event) => setEmail(event.target.value)}
        />

        {error ? (
          <p
            role="alert"
            className="rounded-md border border-danger/30 bg-danger-soft px-4 py-3 text-ui text-danger"
          >
            {error}
          </p>
        ) : null}

        <button
          type="submit"
          disabled={pending}
          className="mt-1 rounded-full bg-fg px-6 py-3 text-ui font-medium text-bg transition-colors hover:bg-white/90 disabled:pointer-events-none disabled:opacity-50"
        >
          {pending ? t('submitting') : t('submit')}
        </button>
      </form>

      <p className="mt-6 text-ui text-dim">
        <Link
          href="/signin"
          className="text-accent-light underline decoration-accent/40 underline-offset-4 hover:decoration-accent"
        >
          {t('backToSignIn')}
        </Link>
      </p>
    </Panel>
  );
}