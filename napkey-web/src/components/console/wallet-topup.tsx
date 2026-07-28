'use client';

import { useCallback, useEffect, useState } from 'react';
import { useLocale, useTranslations } from 'next-intl';
import { api, ApiError } from '@/lib/api/client';
import type { TopupOrderResponse, WalletResponse } from '@/lib/api/types';
import { Badge, Panel, PanelHeader, StatCard } from './ui';
import { creditAmount } from '@/lib/format';
import { creditsFromVnd, microcreditsFromVnd } from '@/lib/pricing';

const PRESETS = [60_000, 120_000, 300_000, 600_000];

export function WalletTopup() {
  const t = useTranslations('console.wallet');
  const locale = useLocale();
  const [wallet, setWallet] = useState<WalletResponse['wallet'] | null>(null);
  const [order, setOrder] = useState<TopupOrderResponse['order'] | null>(null);
  const [amount, setAmount] = useState(60_000);
  const [loading, setLoading] = useState(true);
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const loadWallet = useCallback(async () => {
    const response = await api.get<WalletResponse>('/v1/me/wallet');
    setWallet(response.wallet);
  }, []);

  useEffect(() => {
	let active = true;
	async function initialLoad() {
		try {
			await loadWallet();
		} catch {
			if (active) setError(t('loadFailed'));
		} finally {
			if (active) setLoading(false);
		}
	}
	void initialLoad();
	return () => { active = false; };
  }, [loadWallet, t]);

  useEffect(() => {
    if (!order || order.status === 'paid' || order.status === 'cancelled') return;
    const timer = window.setInterval(async () => {
      try {
        const response = await api.get<TopupOrderResponse>(`/v1/me/topups/${order.id}`);
        setOrder(response.order);
        if (response.order.status === 'paid') await loadWallet();
      } catch {
        // Polling is best-effort; the next tick retries without disrupting the transfer screen.
      }
    }, 3000);
    return () => window.clearInterval(timer);
  }, [loadWallet, order]);

  async function createOrder(event: React.FormEvent) {
    event.preventDefault();
    setPending(true);
    setError(null);
    try {
      const response = await api.post<TopupOrderResponse>('/v1/me/topups', { amountVnd: amount });
      setOrder(response.order);
    } catch (err) {
      setError(err instanceof ApiError ? err.message : t('createFailed'));
    } finally {
      setPending(false);
    }
  }

  if (loading) return <p role="status" className="text-ui text-dim">{t('loading')}</p>;

  return (
    <div className="flex flex-col gap-6">
      <div className="grid gap-4 sm:grid-cols-3">
        <StatCard label={t('balance')} value={creditAmount(wallet?.credits.available, locale)} hint={t('creditRate')} tone="accent" />
        <StatCard label={t('available')} value={wallet?.available.formatted ?? '—'} hint={t('availableHint')} />
        <StatCard label={t('held')} value={wallet?.held.formatted ?? '—'} hint={wallet ? creditAmount(wallet.credits.held, locale) : t('heldHint')} />
      </div>

      {!order ? (
        <Panel as="section">
          <PanelHeader title={t('topupTitle')} description={t('topupDescription')} />
          <form onSubmit={createOrder} className="space-y-5 p-5">
            <div className="flex flex-wrap gap-2">
              {PRESETS.map((value) => <button key={value} type="button" onClick={() => setAmount(value)} className={`rounded-full border px-4 py-2 text-ui tabular-nums ${amount === value ? 'border-accent bg-accent-soft text-accent-light' : 'border-line text-muted hover:text-fg'}`}>{value.toLocaleString('vi-VN')} đ</button>)}
            </div>
            <label className="block max-w-sm text-ui text-muted">{t('customAmount')}<input type="number" min={20000} max={1000000000} step={1000} value={amount} onChange={(event) => setAmount(Number(event.target.value))} className="mt-2 w-full rounded-md border border-line bg-black px-4 py-3 text-fg outline-none focus:border-accent" /></label>
            <p className="font-mono text-ui text-accent-light">{t('youReceive', { credits: creditAmount({ micros: microcreditsFromVnd(amount), credits: creditsFromVnd(amount) }, locale) })}</p>
            <p className="max-w-2xl rounded-md border border-warn/30 bg-warn/10 px-4 py-3 text-ui leading-relaxed text-warn">{t('nonRefundable')}</p>
            {error ? <p role="alert" className="text-ui text-danger">{error}</p> : null}
            <button disabled={pending || amount < 20000} className="rounded-full bg-fg px-6 py-2.5 text-ui font-medium text-bg disabled:opacity-50">{pending ? t('creating') : t('create')}</button>
          </form>
        </Panel>
      ) : (
        <Panel as="section">
          <PanelHeader title={t('transferTitle')} description={t('transferDescription')} action={<Badge tone={order.status === 'paid' ? 'accent' : order.status === 'underpaid' ? 'warn' : 'info'}>{t(`status.${order.status}`)}</Badge>} />
          <div className="grid gap-6 p-5 md:grid-cols-[minmax(0,1fr)_280px]">
            <dl className="divide-y divide-line rounded-lg border border-line">
              {([{ label: t('provider'), value: 'PayOS' }, { label: t('amount'), value: order.expectedAmount.formatted }, { label: t('credits'), value: creditAmount(order.expectedCredits, locale) }, { label: t('memo'), value: order.memoCode }]).map(({ label, value }) => <div key={label} className="flex items-center justify-between gap-4 px-4 py-3"><dt className="text-ui text-dim">{label}</dt><dd className="text-right font-mono text-ui text-fg">{value}</dd></div>)}
            </dl>
            <div className="flex flex-col justify-center rounded-lg border border-line bg-surface-hover p-5 text-center">
              {order.status === 'paid' ? null : <a href={order.payment.checkoutUrl} target="_blank" rel="noreferrer" className="rounded-full bg-fg px-6 py-3 text-ui font-medium text-bg">{t('openCheckout')}</a>}
              <p className="mt-3 text-ui leading-relaxed text-dim">{order.status === 'paid' ? t('paidNotice') : t('pollingNotice')}</p>
            </div>
          </div>
          <div className="border-t border-line px-5 py-4"><button type="button" onClick={() => setOrder(null)} className="text-ui text-muted hover:text-fg">{t('newOrder')}</button></div>
        </Panel>
      )}
    </div>
  );
}
