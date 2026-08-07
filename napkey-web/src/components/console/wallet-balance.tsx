'use client';

import { useLocale, useTranslations } from 'next-intl';
import type { WalletResponse } from '@/lib/api/types';
import { money } from '@/lib/format';
import { Panel } from './ui';

/**
 * The wallet, stated the way a customer needs to read it.
 *
 * Three numbers, not one, and not four of equal weight. Available is what can be spent
 * right now and gets the display size. Balance and held are the arithmetic behind it,
 * because "held" is the entire reason available differs from balance: a request in
 * flight reserves money before the upstream reports what it actually used.
 *
 * A customer who sees only a balance, spends it, and gets refused has no way to know
 * why. Naming the held amount turns a confusing refusal into an explained one.
 *
 * Amounts are shown in VND, not credits. Credits are the unit a top-up is denominated
 * in, but every request since 2026-08-05 is priced from tokens and reports zero
 * credits -- so a credit figure here reads as 0 against real spending.
 */
export function WalletBalance({
  wallet,
}: {
  wallet: WalletResponse['wallet'] | null | undefined;
}) {
  const t = useTranslations('console.wallet.balanceCard');
  const locale = useLocale();
  const promotionalExpiry = wallet?.credits.promotionalExpiresAt;

  return (
    <Panel as="section" className="p-5 sm:p-6">
      <div className="grid gap-6 lg:grid-cols-[minmax(0,1fr)_auto] lg:items-start">
        <div>
          <p className="font-mono text-label tracking-[0.14em] text-dim uppercase">
            {t('availableLabel')}
          </p>
          <p className="mt-2 font-display text-4xl tracking-[-0.035em] tabular-nums text-accent-light sm:text-5xl">
            {money(wallet?.available)}
          </p>
          <p className="mt-2 text-ui leading-relaxed text-muted">{t('availableHint')}</p>
        </div>

        <dl className="grid gap-3 border-t border-line pt-4 text-ui sm:grid-cols-2 lg:min-w-64 lg:grid-cols-1 lg:border-t-0 lg:border-l lg:pt-0 lg:pl-6">
          <Line label={t('balanceLabel')} value={money(wallet?.balance)} hint={t('balanceHint')} />
          <Line
            label={t('heldLabel')}
            value={money(wallet?.held)}
            hint={t('heldHint')}
            tone={(wallet?.held.micros ?? 0) > 0 ? 'warn' : 'dim'}
          />
        </dl>
      </div>

      {(wallet?.credits.promotional.micros ?? 0) > 0 ? (
        <p className="mt-5 rounded-md border border-accent/25 bg-accent-soft px-4 py-3 text-ui leading-relaxed text-accent-light">
          {promotionalExpiry
            ? t('promotionalWithExpiry', {
                amount: money(wallet?.balance),
                date: new Intl.DateTimeFormat(locale === 'vi' ? 'vi-VN' : 'en-US', {
                  dateStyle: 'medium',
                }).format(new Date(promotionalExpiry)),
              })
            : t('promotional')}
        </p>
      ) : null}
    </Panel>
  );
}

function Line({
  label,
  value,
  hint,
  tone = 'dim',
}: {
  label: string;
  value: string;
  hint: string;
  tone?: 'dim' | 'warn';
}) {
  return (
    <div>
      <div className="flex items-baseline justify-between gap-4">
        <dt className="text-muted">{label}</dt>
        <dd
          className={`font-mono tabular-nums ${tone === 'warn' ? 'text-warn' : 'text-fg'}`}
        >
          {value}
        </dd>
      </div>
      <p className="mt-1 text-ui leading-snug text-dim">{hint}</p>
    </div>
  );
}
