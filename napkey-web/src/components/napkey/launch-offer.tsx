'use client';

import { useEffect, useState } from 'react';
import { useTranslations } from 'next-intl';
import { CloseIcon } from '@/components/ui/icon';
import { ButtonLink } from '@/components/ui/button';
import { useSession } from '@/components/console/session-provider';

const DISMISS_KEY = 'napkey-launch-offer-dismissed-at';
const DISMISS_DAYS = 7;

/** Chi hien sau khi nguoi doc da di qua phan gia, khong chan hero. */
const REVEAL_AT = 0.35;

function recentlyDismissed(): boolean {
  try {
    const dismissedAt = Number(window.localStorage.getItem(DISMISS_KEY));
    return Boolean(dismissedAt && Date.now() - dismissedAt < DISMISS_DAYS * 86_400_000);
  } catch {
    return false;
  }
}

function rememberDismissal() {
  try {
    window.localStorage.setItem(DISMISS_KEY, String(Date.now()));
  } catch {
    /* Storage may be disabled. */
  }
}

/**
 * Uu dai ra mat, dang thanh sticky duoi day.
 *
 * Truoc day day la mot modal `aria-modal` bung ra sau 5 giay va khoa scroll.
 * No de len chinh thu nguoi dung vua den de doc, va bat ho tuong tac truoc khi
 * biet NapKey la gi. Mot thanh sticky giu nguyen uu dai va vi tri de thay ma
 * khong cat mach doc: khong khoa scroll, khong bat focus, khong can focus trap.
 *
 * Van hoan thanh nghia vu a11y cua mot vung thong bao: `role="region"` co nhan,
 * nut dong dat kich thuoc bam toi thieu, va Escape dong duoc khi focus dang o
 * trong thanh.
 */
export function LaunchOffer() {
  const t = useTranslations('launchOffer');
  const session = useSession();
  const [open, setOpen] = useState(false);

  useEffect(() => {
    if (recentlyDismissed()) return;

    function onScroll() {
      const scrollable = document.documentElement.scrollHeight - window.innerHeight;
      if (scrollable <= 0) return;
      if (window.scrollY / scrollable >= REVEAL_AT && !recentlyDismissed()) setOpen(true);
    }

    onScroll();
    window.addEventListener('scroll', onScroll, { passive: true });
    return () => window.removeEventListener('scroll', onScroll);
  }, []);

  // Thanh fixed khong chiem cho trong luong, nen no che mat cuoi footer. Day
  // day trang xuong dung chieu cao thanh trong luc no hien, va tra lai khi dong.
  useEffect(() => {
    if (!open) return;
    const previous = document.body.style.paddingBottom;
    document.body.style.paddingBottom = 'var(--launch-offer-height)';
    return () => {
      document.body.style.paddingBottom = previous;
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
      role="region"
      aria-label={t('eyebrow')}
      onKeyDown={(event) => {
        if (event.key === 'Escape') dismiss();
      }}
      className="fixed inset-x-0 bottom-0 z-100 border-t border-accent/30 bg-black/92 backdrop-blur-md motion-safe:animate-in motion-safe:slide-in-from-bottom-4"
    >
      <div className="container-page relative">
        <div className="flex flex-col gap-4 py-4 pb-[max(1rem,env(safe-area-inset-bottom))] sm:flex-row sm:items-center sm:gap-6 sm:py-3.5">
          <div className="min-w-0 flex-1 pr-10 sm:pr-0">
            <p className="font-mono text-micro tracking-[0.16em] text-accent-light uppercase">
              {t('eyebrow')}
            </p>
            <p className="mt-1.5 text-prose font-medium text-fg">{t('title')}</p>
            <p className="mt-0.5 text-ui text-muted">{t('example')}</p>
          </div>

          <div className="flex shrink-0 items-center gap-3">
            <ButtonLink
              href={href}
              variant="pill"
              onClick={rememberDismissal}
              aria-disabled={sessionLoading}
              tabIndex={sessionLoading ? -1 : undefined}
              className={`flex-1 sm:flex-none ${sessionLoading ? 'pointer-events-none opacity-60' : ''}`}
            >
              {sessionLoading
                ? t('ctaLoading')
                : session.status === 'authenticated'
                  ? t('ctaAuthenticated')
                  : t('ctaAnonymous')}
            </ButtonLink>
            <button
              type="button"
              onClick={dismiss}
              aria-label={t('close')}
              className="absolute right-4 top-4 grid size-11 place-items-center rounded-full border border-line bg-surface text-muted transition-colors hover:text-fg sm:static sm:size-10"
            >
              <CloseIcon className="size-4" />
            </button>
          </div>
        </div>
      </div>
    </div>
  );
}
