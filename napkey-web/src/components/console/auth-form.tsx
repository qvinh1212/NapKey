'use client';

import { useEffect, useState } from 'react';
import { useLocale, useTranslations } from 'next-intl';
import { Link, useRouter } from '@/i18n/navigation';
import { api, ApiError } from '@/lib/api/client';
import { Field } from './field';
import { useSession } from './session-provider';
import { Panel } from './ui';
import { shouldRedirectFromSignIn } from '@/lib/session-ui';
import { googleAuthPath, googleErrorKey } from '@/lib/google-auth';

/**
 * Form dang nhap va dang ky.
 *
 * Mot component cho ca hai vi chung chi khac endpoint va nhan chu. Tach thanh hai
 * file se nhan ban toan bo phan xu ly loi theo field ma khong duoc gi.
 *
 * napkey-core tra 202 khi dang ky thanh cong: tai khoan da tao nhung chua xac minh
 * email. Nen sau khi dang ky KHONG dieu huong vao console - o do chua lam duoc gi
 * cho den khi email duoc xac minh.
 */

type Mode = 'signin' | 'signup';

export function AuthForm({ mode, oauthError }: { mode: Mode; oauthError?: string }) {
  const t = useTranslations('console.auth');
  const locale = useLocale();
  const router = useRouter();
  const session = useSession();
  // napkey-core dieu huong ve day voi ?oauth_error= khi luong Google that bai. Ma do
  // di qua URL nen phai qua googleErrorKey truoc khi thanh chu tren trang.
  const oauthErrorKey = googleErrorKey(oauthError);

  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [fieldErrors, setFieldErrors] = useState<Record<string, string>>({});
  const [registered, setRegistered] = useState(false);

  useEffect(() => {
    if (mode === 'signin' && shouldRedirectFromSignIn(session.status)) {
      router.replace('/console');
    }
  }, [mode, router, session.status]);

  async function submit(event: React.FormEvent) {
    event.preventDefault();
    setPending(true);
    setError(null);
    setFieldErrors({});

    try {
      if (mode === 'signup') {
        // `locale` quyet dinh ngon ngu cua email xac minh. Khong gui thi backend mac
        // dinh tieng Viet, va nguoi dung dang doc ban tieng Anh se nhan email tieng Viet.
        await api.post('/v1/auth/register', { email: email.trim(), password, locale });
        setRegistered(true);
        return;
      }

      await api.post('/v1/auth/login', { email: email.trim(), password });
      // Doc lai phien de provider co user truoc khi console mount, tranh mot nhay
      // man hinh "dang kiem tra phien".
      await session.refresh();
      router.replace('/console');
    } catch (err) {
      if (err instanceof ApiError) {
        if (err.fields) setFieldErrors(err.fields);
        setError(err.message);
      } else {
        setError(t('genericError'));
      }
    } finally {
      setPending(false);
    }
  }

  if (registered) {
    return (
      <Panel className="p-8 text-center">
        <h1 className="text-xl tracking-[-0.02em]">{t('checkEmailTitle')}</h1>
        <p className="mx-auto mt-3 max-w-sm text-ui leading-relaxed text-muted">
          {t('checkEmailBody', { email: email.trim() })}
        </p>
        <div className="mt-6 flex flex-wrap justify-center gap-3">
          <Link
            href="/signin"
            className="rounded-full border border-line px-5 py-2 text-ui text-muted transition-colors hover:bg-white/10 hover:text-fg"
          >
            {t('backToSignIn')}
          </Link>
          <Link
            href="/resend-verification"
            className="rounded-full border border-line px-5 py-2 text-ui text-muted transition-colors hover:bg-white/10 hover:text-fg"
          >
            {t('resendVerification')}
          </Link>
        </div>
      </Panel>
    );
  }

  if (mode === 'signin' && session.status !== 'anonymous') return null;

  return (
    <Panel className="p-8">
      <h1 className="text-xl tracking-[-0.02em]">{t(`${mode}.title`)}</h1>
      <p className="mt-2 text-ui text-dim">{t(`${mode}.subtitle`)}</p>

      {oauthErrorKey ? (
        <p
          role="alert"
          className="mt-6 rounded-md border border-danger/30 bg-danger-soft px-4 py-3 text-ui text-danger"
        >
          {t(`googleError.${oauthErrorKey}`)}
        </p>
      ) : null}

      <a
        href={googleAuthPath(locale)}
        className={`${oauthErrorKey ? 'mt-4' : 'mt-6'} flex items-center justify-center gap-3 rounded-full border border-line bg-white px-6 py-3 text-ui font-medium text-black transition-colors hover:bg-white/90`}
      >
        <svg aria-hidden="true" viewBox="0 0 24 24" className="h-5 w-5">
          <path fill="#4285F4" d="M21.6 12.23c0-.71-.06-1.4-.18-2.07H12v3.92h5.38a4.6 4.6 0 0 1-2 3.02v2.54h3.24c1.9-1.75 2.98-4.33 2.98-7.41Z" />
          <path fill="#34A853" d="M12 22c2.7 0 4.98-.9 6.64-2.36l-3.24-2.54c-.9.6-2.05.96-3.4.96-2.61 0-4.82-1.76-5.61-4.13H3.04v2.62A10 10 0 0 0 12 22Z" />
          <path fill="#FBBC05" d="M6.39 13.93A6.02 6.02 0 0 1 6.08 12c0-.67.12-1.32.31-1.93V7.45H3.04A10 10 0 0 0 2 12c0 1.61.38 3.14 1.04 4.55l3.35-2.62Z" />
          <path fill="#EA4335" d="M12 5.94c1.47 0 2.79.5 3.83 1.5l2.88-2.88A9.65 9.65 0 0 0 12 2a10 10 0 0 0-8.96 5.45l3.35 2.62C7.18 7.7 9.39 5.94 12 5.94Z" />
        </svg>
        {t('google')}
      </a>

      <div className="my-5 flex items-center gap-3" aria-hidden="true">
        <span className="h-px flex-1 bg-line" />
        <span className="text-xs uppercase tracking-[0.18em] text-dim">{t('or')}</span>
        <span className="h-px flex-1 bg-line" />
      </div>

      <form onSubmit={submit} className="flex flex-col gap-4" noValidate>
        <Field
          id="email"
          type="email"
          required
          autoComplete="email"
          label={t('emailLabel')}
          error={fieldErrors.email}
          value={email}
          onChange={(event) => setEmail(event.target.value)}
        />
        <Field
          id="password"
          type="password"
          required
          // Trinh quan ly mat khau can biet day la mat khau moi hay cu de goi y dung.
          autoComplete={mode === 'signup' ? 'new-password' : 'current-password'}
          label={t('passwordLabel')}
          hint={mode === 'signup' ? t('passwordHint') : undefined}
          error={fieldErrors.password}
          value={password}
          onChange={(event) => setPassword(event.target.value)}
        />

        {/* Loi chung dat trong role=alert de trinh doc man hinh doc ngay. */}
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
          {pending ? t('submitting') : t(`${mode}.submit`)}
        </button>
      </form>

      <div className="mt-6 flex flex-col gap-2 text-ui text-dim">
        <p>
          {mode === 'signin' ? t('noAccount') : t('haveAccount')}{' '}
          <Link
            href={mode === 'signin' ? '/signup' : '/signin'}
            className="text-accent-light underline decoration-accent/40 underline-offset-4 hover:decoration-accent"
          >
            {mode === 'signin' ? t('signup.link') : t('signin.link')}
          </Link>
        </p>
        {mode === 'signin' ? (
          <p>
            <Link
              href="/forgot-password"
              className="underline decoration-line underline-offset-4 hover:text-muted"
            >
              {t('forgotPassword')}
            </Link>
          </p>
        ) : null}
      </div>
    </Panel>
  );
}
