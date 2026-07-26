'use client';

import { useState } from 'react';
import { useTranslations } from 'next-intl';
import { Link, useRouter } from '@/i18n/navigation';
import { api, ApiError } from '@/lib/api/client';
import { Field } from './field';
import { Panel } from './ui';

/**
 * Dat lai mat khau bang token tu email.
 *
 * Duong dan `/{locale}/reset-password?token=...` khop voi `mail/templates.go`.
 *
 * Backend huy TAT CA phien khi doi mat khau qua duong nay: dat lai mat khau la phan
 * ung voi mot tai khoan co the da bi chiem, nen phien cua ke tan cong phai chet
 * cung. Vi vay sau khi xong phai dang nhap lai, khong tu vao console duoc.
 */

export function ResetPassword({ token }: { token: string | null }) {
  const t = useTranslations('console.reset');
  const router = useRouter();

  const [password, setPassword] = useState('');
  const [confirm, setConfirm] = useState('');
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [fieldErrors, setFieldErrors] = useState<Record<string, string>>({});
  const [done, setDone] = useState(false);

  async function submit(event: React.FormEvent) {
    event.preventDefault();

    // Kiem tra khop o phia client: khong can mot chang mang de biet hai o khac nhau.
    if (password !== confirm) {
      setFieldErrors({ confirm: t('mismatch') });
      return;
    }

    setPending(true);
    setError(null);
    setFieldErrors({});
    try {
      await api.post('/v1/auth/reset-password', { token, password });
      setDone(true);
    } catch (err) {
      if (err instanceof ApiError) {
        if (err.fields) setFieldErrors(err.fields);
        setError(err.message);
      } else {
        setError(t('networkError'));
      }
    } finally {
      setPending(false);
    }
  }

  if (!token) {
    return (
      <Panel className="p-8 text-center">
        <h1 className="text-xl tracking-[-0.02em]">{t('missingTokenTitle')}</h1>
        <p className="mx-auto mt-3 max-w-sm text-ui leading-relaxed text-muted">
          {t('missingTokenBody')}
        </p>
        <Link
          href="/forgot-password"
          className="mt-6 inline-flex rounded-full bg-fg px-5 py-2.5 text-ui font-medium text-bg transition-colors hover:bg-white/90"
        >
          {t('requestNew')}
        </Link>
      </Panel>
    );
  }

  if (done) {
    return (
      <Panel className="p-8 text-center">
        <h1 className="text-xl tracking-[-0.02em]">{t('doneTitle')}</h1>
        <p className="mx-auto mt-3 max-w-sm text-ui leading-relaxed text-muted">{t('doneBody')}</p>
        <button
          type="button"
          onClick={() => router.replace('/signin')}
          className="mt-6 rounded-full bg-fg px-6 py-2.5 text-ui font-medium text-bg transition-colors hover:bg-white/90"
        >
          {t('goSignIn')}
        </button>
      </Panel>
    );
  }

  return (
    <Panel className="p-8">
      <h1 className="text-xl tracking-[-0.02em]">{t('title')}</h1>
      <p className="mt-2 text-ui text-dim">{t('subtitle')}</p>

      <form onSubmit={submit} className="mt-6 flex flex-col gap-4" noValidate>
        <Field
          id="new-password"
          type="password"
          required
          autoComplete="new-password"
          label={t('newPassword')}
          hint={t('passwordHint')}
          error={fieldErrors.password}
          value={password}
          onChange={(event) => setPassword(event.target.value)}
        />
        <Field
          id="confirm-password"
          type="password"
          required
          autoComplete="new-password"
          label={t('confirmPassword')}
          error={fieldErrors.confirm}
          value={confirm}
          onChange={(event) => setConfirm(event.target.value)}
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
    </Panel>
  );
}