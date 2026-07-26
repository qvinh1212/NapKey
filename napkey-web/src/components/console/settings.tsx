'use client';

import { useState } from 'react';
import { useLocale, useTranslations } from 'next-intl';
import { api, ApiError } from '@/lib/api/client';
import { dateTime } from '@/lib/format';
import { Field } from './field';
import { useSession } from './session-provider';
import { Badge, Panel, PanelHeader } from './ui';

/**
 * Trang cai dat: thong tin tai khoan va doi mat khau.
 *
 * Doi mat khau qua duong nay KHONG huy cac phien khac (khac voi dat lai mat khau
 * bang email). Nguoi dung con biet mat khau cu, nen day khong phai phan ung voi
 * mot vu chiem tai khoan, va dang xuat het thiet bi cua ho la mot su bat tien
 * khong co ly do.
 */

export function Settings() {
  const t = useTranslations('console.settings');
  const locale = useLocale();
  const session = useSession();

  const [current, setCurrent] = useState('');
  const [next, setNext] = useState('');
  const [confirm, setConfirm] = useState('');
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [fieldErrors, setFieldErrors] = useState<Record<string, string>>({});
  const [done, setDone] = useState(false);

  async function submit(event: React.FormEvent) {
    event.preventDefault();

    if (next !== confirm) {
      setFieldErrors({ confirm: t('mismatch') });
      return;
    }

    setPending(true);
    setError(null);
    setFieldErrors({});
    setDone(false);
    try {
      await api.post('/v1/me/password', { currentPassword: current, newPassword: next });
      setDone(true);
      setCurrent('');
      setNext('');
      setConfirm('');
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

  if (session.status !== 'authenticated') return null;
  const { user } = session;

  return (
    <div className="flex flex-col gap-6">
      <Panel as="section">
        <PanelHeader title={t('accountTitle')} description={t('accountDescription')} />
        <dl className="divide-y divide-line">
          <div className="flex flex-wrap items-center justify-between gap-3 px-5 py-3.5">
            <dt className="text-ui text-dim">{t('email')}</dt>
            <dd className="flex items-center gap-2 text-ui text-fg">
              {user.email}
              {user.emailVerified ? (
                <Badge tone="accent">{t('verified')}</Badge>
              ) : (
                <Badge tone="warn">{t('unverified')}</Badge>
              )}
            </dd>
          </div>
          <div className="flex flex-wrap items-center justify-between gap-3 px-5 py-3.5">
            <dt className="text-ui text-dim">{t('memberSince')}</dt>
            <dd className="text-ui text-muted">{dateTime(user.createdAt, locale)}</dd>
          </div>
          <div className="flex flex-wrap items-center justify-between gap-3 px-5 py-3.5">
            <dt className="text-ui text-dim">{t('sessionExpires')}</dt>
            <dd className="text-ui text-muted">{dateTime(session.expiresAt, locale)}</dd>
          </div>
        </dl>
      </Panel>

      <Panel as="section">
        <PanelHeader title={t('passwordTitle')} description={t('passwordDescription')} />
        <form onSubmit={submit} className="flex max-w-md flex-col gap-4 px-5 py-5" noValidate>
          <Field
            id="current-password"
            type="password"
            required
            autoComplete="current-password"
            label={t('currentPassword')}
            error={fieldErrors.currentPassword}
            value={current}
            onChange={(event) => setCurrent(event.target.value)}
          />
          <Field
            id="next-password"
            type="password"
            required
            autoComplete="new-password"
            label={t('newPassword')}
            hint={t('passwordHint')}
            error={fieldErrors.newPassword}
            value={next}
            onChange={(event) => setNext(event.target.value)}
          />
          <Field
            id="confirm-new-password"
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

          {done ? (
            <p
              role="status"
              className="rounded-md border border-accent/30 bg-accent-soft px-4 py-3 text-ui text-accent-light"
            >
              {t('passwordUpdated')}
            </p>
          ) : null}

          <button
            type="submit"
            disabled={pending}
            className="mt-1 self-start rounded-full bg-fg px-6 py-2.5 text-ui font-medium text-bg transition-colors hover:bg-white/90 disabled:pointer-events-none disabled:opacity-50"
          >
            {pending ? t('submitting') : t('submit')}
          </button>
        </form>
      </Panel>
    </div>
  );
}