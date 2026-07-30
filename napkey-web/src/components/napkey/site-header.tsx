'use client';

import { useEffect, useRef, useState } from 'react';
import { useTranslations } from 'next-intl';
import { Link } from '@/i18n/navigation';
import { ButtonLink } from '@/components/ui/button';
import { useSession } from '@/components/console/session-provider';
import { publicAuthAction } from '@/lib/session-ui';
import { Logo } from './logo';
import { LocaleSwitcher } from './locale-switcher';

const sections = [
  { key: 'pricing', href: '/#pricing' },
  { key: 'integrate', href: '/#integrate' },
  { key: 'billing', href: '/#billing' },
  { key: 'docs', href: '/docs' },
  { key: 'trust', href: '/#trust' },
  { key: 'compatibility', href: '/compatibility' },
] as const;

export function SiteHeader() {
  const t = useTranslations('nav');
  const session = useSession();
  const authAction = publicAuthAction(session.status);
  const [open, setOpen] = useState(false);
  const menuButtonRef = useRef<HTMLButtonElement>(null);
  const mobileNavRef = useRef<HTMLElement>(null);

  // Chan scroll khi menu mobile dang mo.
  useEffect(() => {
    const previousOverflow = document.body.style.overflow;
    const desktop = window.matchMedia('(min-width: 64rem)');
    document.body.style.overflow = open ? 'hidden' : '';
    if (open) {
      mobileNavRef.current?.querySelector<HTMLElement>('a, button')?.focus();
    }
    function onKeyDown(event: KeyboardEvent) {
      if (event.key !== 'Escape' || !open) return;
      setOpen(false);
      menuButtonRef.current?.focus();
    }
    function onDesktop(event: MediaQueryListEvent) {
      if (event.matches) setOpen(false);
    }
    document.addEventListener('keydown', onKeyDown);
    desktop.addEventListener('change', onDesktop);
    return () => {
      document.body.style.overflow = previousOverflow;
      document.removeEventListener('keydown', onKeyDown);
      desktop.removeEventListener('change', onDesktop);
    };
  }, [open]);

  return (
    <header className="fixed inset-x-0 top-0 z-50 pt-4 sm:pt-6">
      <div className="container-page">
        <div className="flex items-center justify-between gap-3 sm:gap-6">
          <Link
            href="/"
            className="rounded-sm focus-visible:outline-2 focus-visible:outline-offset-4 focus-visible:outline-accent"
          >
            <Logo />
          </Link>

          <nav aria-label="Primary" className="hidden items-center gap-1 lg:flex">
            {sections.map(({ key, href }) => (
              <Link
                key={key}
                href={href}
                className="rounded-full px-4 py-2 text-ui text-subtle transition-colors duration-150 ease-[var(--ease-smooth)] hover:bg-surface-hover hover:text-fg"
              >
                {t(key)}
              </Link>
            ))}
          </nav>

          <div className="flex items-center gap-3">
            <LocaleSwitcher />
            {authAction ? (
              <Link
                href={authAction.href}
                className="hidden rounded-full px-4 py-2 text-ui text-subtle transition-colors duration-150 ease-[var(--ease-smooth)] hover:bg-surface-hover hover:text-fg sm:inline-flex"
              >
                {t(authAction.labelKey)}
              </Link>
            ) : null}
            {session.status === 'anonymous' ? (
              <ButtonLink href="/signup" variant="pill" className="hidden sm:inline-flex">
                {t('getStarted')}
              </ButtonLink>
            ) : null}
            <button
              ref={menuButtonRef}
              type="button"
              aria-expanded={open}
              aria-controls="mobile-nav"
              aria-label={open ? t('closeMenu') : t('openMenu')}
              onClick={() => setOpen((v) => !v)}
              className="inline-flex size-11 items-center justify-center rounded-full border border-line bg-black/80 text-muted backdrop-blur lg:hidden"
            >
              <span aria-hidden className="text-base leading-none">
                {open ? '\u2715' : '\u2261'}
              </span>
            </button>
          </div>
        </div>

        {open ? (
          <nav
            ref={mobileNavRef}
            id="mobile-nav"
            aria-label="Mobile"
            className="mt-3 flex max-h-[calc(100dvh-5.5rem)] flex-col gap-1 overflow-y-auto rounded-xl border border-line bg-black/95 p-4 shadow-2xl backdrop-blur lg:hidden"
          >
            {sections.map(({ key, href }) => (
              <Link
                key={key}
                href={href}
                onClick={() => setOpen(false)}
                className="rounded-md px-3 py-3 text-base text-muted transition-colors hover:bg-surface-hover hover:text-fg"
              >
                {t(key)}
              </Link>
            ))}
            {authAction ? (
              <Link
                href={authAction.href}
                onClick={() => setOpen(false)}
                className="rounded-md px-3 py-3 text-base text-muted transition-colors hover:bg-surface-hover hover:text-fg"
              >
                {t(authAction.labelKey)}
              </Link>
            ) : null}
            {session.status === 'anonymous' ? (
              <ButtonLink href="/signup" className="mt-3 w-full" onClick={() => setOpen(false)}>
                {t('getStarted')}
              </ButtonLink>
            ) : null}
          </nav>
        ) : null}
      </div>
    </header>
  );
}
