'use client';

import { useEffect, useRef, useState } from 'react';
import { useTranslations } from 'next-intl';
import { Link } from '@/i18n/navigation';
import { ButtonLink } from '@/components/ui/button';
import { Logo } from './logo';
import { LocaleSwitcher } from './locale-switcher';

const sections = [
  { key: 'pricing', href: '/#pricing' },
  { key: 'integrate', href: '/#integrate' },
  { key: 'billing', href: '/#billing' },
  { key: 'trust', href: '/#trust' },
] as const;

export function SiteHeader() {
  const t = useTranslations('nav');
  const [open, setOpen] = useState(false);
  const menuButtonRef = useRef<HTMLButtonElement>(null);
  const mobileNavRef = useRef<HTMLElement>(null);

  // Chan scroll khi menu mobile dang mo.
  useEffect(() => {
    document.body.style.overflow = open ? 'hidden' : '';
    if (open) {
      mobileNavRef.current?.querySelector<HTMLElement>('a, button')?.focus();
    }
    function onKeyDown(event: KeyboardEvent) {
      if (event.key !== 'Escape' || !open) return;
      setOpen(false);
      menuButtonRef.current?.focus();
    }
    document.addEventListener('keydown', onKeyDown);
    return () => {
      document.body.style.overflow = '';
      document.removeEventListener('keydown', onKeyDown);
    };
  }, [open]);

  return (
    <header className="fixed inset-x-0 top-0 z-50 pt-6">
      <div className="container-page">
        <div className="flex items-center justify-between gap-6">
          <Link
            href="/"
            className="rounded-sm focus-visible:outline-2 focus-visible:outline-offset-4 focus-visible:outline-accent"
          >
            <Logo />
          </Link>

          <nav aria-label="Primary" className="hidden items-center gap-1 md:flex">
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
            <Link
              href="/signin"
              className="hidden rounded-full px-4 py-2 text-ui text-subtle transition-colors duration-150 ease-[var(--ease-smooth)] hover:bg-surface-hover hover:text-fg sm:inline-flex"
            >
              {t('signIn')}
            </Link>
            <ButtonLink href="/signup" variant="pill" className="hidden sm:inline-flex">
              {t('getStarted')}
            </ButtonLink>
            <button
              ref={menuButtonRef}
              type="button"
              aria-expanded={open}
              aria-controls="mobile-nav"
              aria-label={open ? t('closeMenu') : t('openMenu')}
              onClick={() => setOpen((v) => !v)}
              className="inline-flex size-9 items-center justify-center rounded-full border border-line bg-surface-hover text-muted md:hidden"
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
            className="mt-4 flex flex-col gap-1 rounded-xl border border-line bg-black/95 p-4 backdrop-blur md:hidden"
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
            <Link
              href="/signin"
              onClick={() => setOpen(false)}
              className="rounded-md px-3 py-3 text-base text-muted transition-colors hover:bg-surface-hover hover:text-fg"
            >
              {t('signIn')}
            </Link>
            <ButtonLink href="/signup" className="mt-3 w-full" onClick={() => setOpen(false)}>
              {t('getStarted')}
            </ButtonLink>
          </nav>
        ) : null}
      </div>
    </header>
  );
}
