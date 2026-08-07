'use client';

import { useEffect, useRef, useState } from 'react';
import { useTranslations } from 'next-intl';
import { CloseIcon } from '@/components/ui/icon';
import { ButtonLink } from '@/components/ui/button';
import { useSession } from '@/components/console/session-provider';

const DISMISS_KEY = 'napkey-launch-offer-dismissed-at';
const DISMISS_DAYS = 7;

function recentlyDismissed(): boolean {
  try {
    const dismissedAt = Number(window.localStorage.getItem(DISMISS_KEY));
    return Boolean(dismissedAt && Date.now() - dismissedAt < DISMISS_DAYS * 86_400_000);
  } catch {
    return false;
  }
}

function rememberDismissal() {
  try { window.localStorage.setItem(DISMISS_KEY, String(Date.now())); } catch { /* Storage may be disabled. */ }
}

export function LaunchOffer() {
  const t = useTranslations('launchOffer');
  const session = useSession();
  const [open, setOpen] = useState(false);
  const closeRef = useRef<HTMLButtonElement>(null);
  const previousFocus = useRef<HTMLElement | null>(null);

  useEffect(() => {
    if (recentlyDismissed()) return;

    const show = () => { if (!recentlyDismissed()) setOpen(true); };
    const timer = window.setTimeout(show, 5_000);
    const onScroll = () => {
      const scrollable = document.documentElement.scrollHeight - window.innerHeight;
      if (scrollable > 0 && window.scrollY / scrollable >= 0.35) show();
    };
    window.addEventListener('scroll', onScroll, { passive: true });
    return () => {
      window.clearTimeout(timer);
      window.removeEventListener('scroll', onScroll);
    };
  }, []);

  useEffect(() => {
    if (!open) return;
    previousFocus.current = document.activeElement instanceof HTMLElement ? document.activeElement : null;
    const previousOverflow = document.body.style.overflow;
    document.body.style.overflow = 'hidden';
    closeRef.current?.focus();
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') dismiss();
      if (event.key === 'Tab') {
        const dialog = closeRef.current?.closest('[role="dialog"]');
        const focusable = dialog?.querySelectorAll<HTMLElement>('a[href],button:not([disabled]),[tabindex]:not([tabindex="-1"])');
        if (!focusable?.length) return;
        const first = focusable[0]!;
        const last = focusable[focusable.length - 1]!;
        if (event.shiftKey && document.activeElement === first) { event.preventDefault(); last.focus(); }
        if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first.focus(); }
      }
    };
    window.addEventListener('keydown', onKeyDown);
    return () => {
      document.body.style.overflow = previousOverflow;
      window.removeEventListener('keydown', onKeyDown);
      previousFocus.current?.focus();
    };
  }, [open]);

  function dismiss() {
    rememberDismissal();
    setOpen(false);
  }

  if (!open) return null;
  const sessionLoading = session.status === 'loading';
  const href = session.status === 'authenticated' ? '/console/billing' : '/signup';

  return (
    <div
      className="fixed inset-0 z-100 flex items-end justify-center bg-black/75 p-0 backdrop-blur-sm motion-safe:animate-in motion-safe:fade-in sm:items-center sm:p-6"
      onMouseDown={(event) => { if (event.target === event.currentTarget) dismiss(); }}
    >
      <section
        role="dialog"
        aria-modal="true"
        aria-labelledby="launch-offer-title"
        className="relative max-h-[calc(100dvh-1rem)] w-full overflow-y-auto rounded-t-3xl border border-accent/30 bg-bg px-6 pb-[max(1.75rem,env(safe-area-inset-bottom))] pt-8 shadow-2xl motion-safe:animate-in motion-safe:slide-in-from-bottom-6 sm:max-w-2xl sm:rounded-3xl sm:p-10"
      >
        <div aria-hidden="true" className="absolute -right-20 -top-24 h-64 w-64 rounded-full bg-accent/15 blur-3xl" />
        <button
          ref={closeRef}
          type="button"
          onClick={dismiss}
          aria-label={t('close')}
          className="absolute right-4 top-4 grid min-h-11 min-w-11 place-items-center rounded-full border border-line bg-surface text-muted transition-colors hover:text-fg focus-visible:outline-2 focus-visible:outline-accent"
        >
          <CloseIcon className="size-4" />
        </button>
        <p className="relative font-mono text-micro uppercase tracking-[0.18em] text-accent-light">{t('eyebrow')}</p>
        <h2 id="launch-offer-title" className="relative mt-4 max-w-xl font-display text-3xl font-bold leading-tight text-fg sm:text-5xl">
          {t('title')}
        </h2>
        <p className="relative mt-4 max-w-xl text-ui leading-relaxed text-muted sm:text-lg">{t('body')}</p>
        <div className="relative mt-6 grid gap-3 sm:grid-cols-2">
          <div className="rounded-2xl border border-line bg-surface p-4 text-ui text-muted">{t('trial')}</div>
          <div className="rounded-2xl border border-accent/40 bg-accent-soft p-4 text-ui text-accent-light">{t('policy')}</div>
        </div>
        <p className="relative mt-5 font-mono text-xl text-fg sm:text-2xl">{t('example')}</p>
        <div className="relative mt-6 flex flex-col gap-3 sm:flex-row sm:items-center">
          <ButtonLink href={href} onClick={rememberDismissal} aria-disabled={sessionLoading} tabIndex={sessionLoading ? -1 : undefined} className={`w-full sm:w-auto ${sessionLoading ? 'pointer-events-none opacity-60' : ''}`}>
            {sessionLoading ? t('ctaLoading') : session.status === 'authenticated' ? t('ctaAuthenticated') : t('ctaAnonymous')}
          </ButtonLink>
          <p className="text-xs leading-relaxed text-dim sm:max-w-xs">{t('terms')}</p>
        </div>
      </section>
    </div>
  );
}
