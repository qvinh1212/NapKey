import { useTranslations } from 'next-intl';
import { ButtonLink } from '@/components/ui/button';
import type { Money, WalletResponse } from '@/lib/api/types';
import { spendMeter } from '@/lib/spend-meter';
import { money } from '@/lib/format';
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

/**
 * How much of what the customer funded has been spent.
 *
 * Denominated in VND. It used to count credits, and every 9Router request reports zero
 * of those, so the bar sat empty while a wallet drained and the top-up prompt at 70%
 * never appeared. A meter that cannot move is worse than no meter: it reassures.
 */
export function SpendMeter({
  usedAmount,
  wallet,
}: {
  usedAmount: Money;
  wallet: WalletResponse['wallet'];
}) {
  const t = useTranslations('console.overview.spendMeter');
  const meter = spendMeter(usedAmount.micros, wallet.balance.micros, wallet.held.micros);
  const roundedPercent = Math.round(meter.percent * 10) / 10;

  return (
    <Panel as="section" className="p-5">
      <div className="flex flex-wrap items-start justify-between gap-5">
        <div>
          <p className="font-mono text-label tracking-[0.14em] text-dim uppercase">{t('title')}</p>
          <p
            className={`mt-2 font-display text-3xl tracking-[-0.03em] tabular-nums ${valueTone[meter.tone]}`}
          >
            {money(usedAmount)}
          </p>
        </div>
        <div className="text-right text-ui tabular-nums">
          <p className="text-muted">{t('remaining', { amount: money(wallet.available) })}</p>
          <p className="mt-1 text-dim">{t('funded', { amount: money(wallet.balance) })}</p>
        </div>
      </div>

      <div
        role="progressbar"
        aria-label={t('ariaLabel')}
        aria-valuemin={0}
        aria-valuemax={100}
        aria-valuenow={roundedPercent}
        aria-valuetext={t('ariaValue', {
          used: money(usedAmount),
          total: money(wallet.balance),
          percent: roundedPercent,
        })}
        className="mt-5 h-1.5 overflow-hidden rounded-full bg-surface-2"
      >
        <div
          className={`h-full rounded-full transition-[width] duration-700 ease-[var(--ease-smooth)] ${barTone[meter.tone]}`}
          style={{ width: `${meter.percent}%` }}
        />
      </div>

      <div className="mt-3 flex flex-wrap items-center justify-between gap-3 text-ui">
        <p className="text-dim">
          {meter.held > 0 ? t('held', { amount: money(wallet.held) }) : t('noHold')}
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
