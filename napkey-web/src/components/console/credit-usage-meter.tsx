import { useLocale, useTranslations } from 'next-intl';
import { ButtonLink } from '@/components/ui/button';
import type { Money, WalletResponse } from '@/lib/api/types';
import { creditMeter } from '@/lib/credit-meter';
import { count, money } from '@/lib/format';
import { Panel } from './ui';

const barTone = {
  accent: 'bg-accent',
  warn: 'bg-warn',
  danger: 'bg-danger',
} as const;

const valueTone = {
  accent: 'text-accent-light',
  warn: 'text-warn',
  danger: 'text-danger',
} as const;

export function CreditUsageMeter({
  usedCredits,
  usedAmount,
  wallet,
}: {
  usedCredits: number;
  usedAmount: Money;
  wallet: WalletResponse['wallet'];
}) {
  const t = useTranslations('console.overview.creditMeter');
  const locale = useLocale();
  const meter = creditMeter(
    usedCredits,
    wallet.credits.balance.credits,
    wallet.credits.held.credits,
  );
  const roundedPercent = Math.round(meter.percent * 10) / 10;

  return (
    <Panel as="section" className="p-5">
      <div className="flex flex-wrap items-start justify-between gap-5">
        <div>
          <p className="font-mono text-label tracking-[0.14em] text-dim uppercase">
            {t('title')}
          </p>
          <p
            className={`mt-2 font-display text-3xl tracking-[-0.03em] tabular-nums ${valueTone[meter.tone]}`}
          >
            {count(meter.used, locale)}
            <span className="ml-2 font-sans text-sm tracking-normal text-dim">
              {t('unit', { count: meter.used })}
            </span>
          </p>
        </div>
        <div className="text-right text-ui tabular-nums">
          <p className="text-muted">
            {t('remaining', { credits: meter.available })}
          </p>
          <p className="mt-1 text-dim">
            {t('total', { credits: meter.total })}
          </p>
        </div>
      </div>

      <div
        role="progressbar"
        aria-label={t('ariaLabel')}
        aria-valuemin={0}
        aria-valuemax={100}
        aria-valuenow={roundedPercent}
        aria-valuetext={t('ariaValue', {
          used: count(meter.used, locale),
          total: count(meter.total, locale),
          percent: roundedPercent,
        })}
        className="mt-5 h-2.5 overflow-hidden rounded-full bg-white/10"
      >
        <div
          className={`h-full rounded-full transition-[width] duration-700 ease-[var(--ease-smooth)] ${barTone[meter.tone]}`}
          style={{ width: `${meter.percent}%` }}
        />
      </div>

      <div className="mt-3 flex flex-wrap items-center justify-between gap-3 text-ui">
        <p className="text-dim">
          {t('settledCost', { amount: money(usedAmount) })}
          {meter.held > 0
            ? ` · ${t('held', { credits: meter.held })}`
            : ''}
        </p>
        {meter.percent >= 70 ? (
          <ButtonLink href="/console/wallet" variant="pill">
            {t('topUp')}
          </ButtonLink>
        ) : (
          <p className="font-mono text-label text-dim">{roundedPercent}%</p>
        )}
      </div>
    </Panel>
  );
}
