'use client';

import { useCallback, useEffect, useState } from 'react';
import { useLocale, useTranslations } from 'next-intl';
import { api, ApiError } from '@/lib/api/client';
import type { TopupHistoryResponse, TopupOrderResponse, WalletResponse } from '@/lib/api/types';
import { WalletBalance } from './wallet-balance';
import { Badge, Panel, PanelHeader } from './ui';
import { CopyButton } from '@/components/ui/copy-button';
import { creditAmount } from '@/lib/format';
import {
  calculateCreditsCach2,
  CREDIT_PACKAGES_CACH_2,
  formatVnd,
  MIN_TOPUP_VND,
  TOPUP_STEP_VND,
} from '@/lib/pricing';

export function WalletTopup() {
  const t = useTranslations('console.wallet');
  const locale = useLocale();
  const [wallet, setWallet] = useState<WalletResponse['wallet'] | null>(null);
  const [history, setHistory] = useState<TopupHistoryResponse['orders']>([]);
  const [order, setOrder] = useState<TopupOrderResponse['order'] | null>(null);
  const [amount, setAmount] = useState(75_000);
  const [loading, setLoading] = useState(true);
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const loadWallet = useCallback(async () => {
    const response = await api.get<WalletResponse>('/v1/me/wallet');
    setWallet(response.wallet);
  }, []);

  const loadHistory = useCallback(async () => {
    const response = await api.get<TopupHistoryResponse>('/v1/me/topups');
    setHistory(response.orders);
  }, []);

  useEffect(() => {
	let active = true;
	async function initialLoad() {
		try {
			await Promise.all([loadWallet(), loadHistory()]);
		} catch {
			if (active) setError(t('loadFailed'));
		} finally {
			if (active) setLoading(false);
		}
	}
	void initialLoad();
	return () => { active = false; };
  }, [loadHistory, loadWallet, t]);

  useEffect(() => {
    if (!order || order.status === 'paid' || order.status === 'cancelled') return;
    const timer = window.setInterval(async () => {
      try {
        const response = await api.get<TopupOrderResponse>(`/v1/me/topups/${order.id}`);
        setOrder(response.order);
        if (response.order.status === 'paid') await Promise.all([loadWallet(), loadHistory()]);
      } catch {
        // Polling is best-effort; the next tick retries without disrupting the transfer screen.
      }
    }, 3000);
    return () => window.clearInterval(timer);
  }, [loadHistory, loadWallet, order]);

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
      <WalletBalance wallet={wallet} />

      {!order ? (
        <Panel as="section">
          <PanelHeader title={t('topupTitle')} description={t('topupDescription')} />
          <form onSubmit={createOrder} className="space-y-6 p-5 sm:p-6">
            <div>
              <div className="flex items-center justify-between">
                <label className="block text-ui font-medium text-fg">
                  Chọn gói nạp Credits (Tỉ giá 1 CR = 75 ₫)
                </label>
                <span className="font-mono text-micro text-accent-light">1 CR = 75 ₫</span>
              </div>
              <div className="mt-3 grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-4">
                {CREDIT_PACKAGES_CACH_2.map((pkg) => (
                  <button
                    key={pkg.vnd}
                    type="button"
                    onClick={() => setAmount(pkg.vnd)}
                    className={`relative rounded-xl border p-4 text-left transition-all ${
                      amount === pkg.vnd
                        ? 'border-accent bg-accent-soft text-fg ring-1 ring-accent shadow-[0_0_20px_rgba(16,185,129,0.15)]'
                        : 'border-line bg-surface hover:border-accent/40 hover:bg-surface-hover'
                    }`}
                  >
                    {pkg.popular ? (
                      <span className="absolute top-2.5 right-2.5 rounded-full bg-accent/20 px-2 py-0.5 font-mono text-[10px] font-bold tracking-wider text-accent-light">
                        POPULAR
                      </span>
                    ) : null}
                    <p className="font-mono text-lg font-bold text-accent-light">
                      {pkg.credits.toLocaleString('vi-VN')} CR
                    </p>
                    <p className="mt-1 font-mono text-ui text-fg font-medium">
                      {formatVnd(pkg.vnd, locale)}
                    </p>
                    <p className="mt-1 font-mono text-micro text-dim">{pkg.label}</p>
                  </button>
                ))}
              </div>
            </div>

            <div className="max-w-md">
              <label htmlFor="wallet-topup-custom-amount" className="block text-label text-dim">
                {t('customAmount')}
              </label>
              <div className="mt-1.5 flex items-center gap-3">
                <input
                  id="wallet-topup-custom-amount"
                  type="number"
                  min={MIN_TOPUP_VND}
                  max={1000000000}
                  step={TOPUP_STEP_VND}
                  value={amount}
                  onChange={(event) => setAmount(Number(event.target.value))}
                  className="w-full rounded-lg border border-line bg-surface px-4 py-2.5 font-mono text-ui text-fg outline-none focus:border-accent"
                />
              </div>
              <p className="mt-2 font-mono text-label text-accent-light">
                Bạn nhận được:{' '}
                <span className="font-bold">
                  {calculateCreditsCach2(amount).toLocaleString('vi-VN')} CR
                </span>{' '}
                <span className="text-dim">(Tỉ giá 1 CR = 75 ₫)</span>
              </p>
            </div>

            <p className="max-w-2xl rounded-md border border-warn/30 bg-warn/10 px-4 py-3 text-ui leading-relaxed text-warn">
              {t('nonRefundable')}
            </p>
            {error ? (
              <p role="alert" className="text-ui text-danger">
                {error}
              </p>
            ) : null}
            <button
              type="submit"
              disabled={pending || amount < MIN_TOPUP_VND || amount % TOPUP_STEP_VND !== 0}
              className="rounded-full bg-fg px-6 py-2.5 text-ui font-medium text-bg transition-colors hover:bg-white/90 disabled:opacity-50"
            >
              {pending ? t('creating') : t('create')}
            </button>
          </form>
        </Panel>
      ) : (
        <Panel as="section">
          <PanelHeader title={t('transferTitle')} description={t('transferDescription')} action={<Badge tone={order.status === 'paid' ? 'accent' : order.status === 'underpaid' ? 'warn' : 'info'}>{t(`status.${order.status}`)}</Badge>} />
          <div className="grid gap-6 p-5 md:grid-cols-[minmax(0,1fr)_280px]">
            <dl className="divide-y divide-line rounded-lg border border-line">
              {([
                { label: t('provider'), value: 'PayOS', copyable: false },
                { label: t('amount'), value: order.expectedAmount.formatted, raw: String(order.expectedAmount.vnd), copyable: true },
                { label: t('credits'), value: creditAmount(order.expectedCredits, locale), copyable: false },
                { label: t('memo'), value: order.memoCode, raw: order.memoCode, copyable: true, highlight: true },
              ]).map(({ label, value, raw, copyable, highlight }) => (
                <div key={label} className="flex items-center justify-between gap-4 px-4 py-3">
                  <dt className="text-ui text-dim">{label}</dt>
                  <dd className="flex items-center gap-2 text-right font-mono text-ui text-fg">
                    <span className={highlight ? 'font-semibold text-accent-light' : ''}>{value}</span>
                    {copyable ? (
                      <CopyButton
                        value={raw || value}
                        variant="icon"
                        showTooltip
                        copiedLabel={t('copied') || 'Copied!'}
                        className="size-6 text-muted hover:text-fg"
                      />
                    ) : null}
                  </dd>
                </div>
              ))}
            </dl>
            <div className="flex flex-col justify-center rounded-lg border border-line bg-surface-hover p-5 text-center">
              {order.status === 'paid' ? null : <a href={order.payment.checkoutUrl} target="_blank" rel="noreferrer" className="rounded-full bg-fg px-6 py-3 text-ui font-medium text-bg">{t('openCheckout')}</a>}
              <p className="mt-3 text-ui leading-relaxed text-dim">{order.status === 'paid' ? t('paidNotice') : t('pollingNotice')}</p>
            </div>
          </div>
          <div className="border-t border-line px-5 py-4"><button type="button" onClick={() => setOrder(null)} className="text-ui text-muted hover:text-fg">{t('newOrder')}</button></div>
        </Panel>
      )}

      <Panel as="section">
        <PanelHeader title={t('historyTitle')} description={t('historyDescription')} />
        {history.length === 0 ? (
          <p className="p-5 text-ui text-dim">{t('historyEmpty')}</p>
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full min-w-[640px] text-left text-ui">
              <thead className="border-b border-line text-dim">
                <tr><th className="px-5 py-3 font-medium">{t('historyTime')}</th><th className="px-5 py-3 font-medium">{t('memo')}</th><th className="px-5 py-3 font-medium">{t('amount')}</th><th className="px-5 py-3 font-medium">{t('credits')}</th><th className="px-5 py-3 font-medium">{t('historyStatus')}</th></tr>
              </thead>
              <tbody className="divide-y divide-line">
                {history.map((item) => (
                  <tr key={item.id}>
                    <td className="px-5 py-3 text-muted">{new Intl.DateTimeFormat(locale, { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(item.createdAt))}</td>
                    <td className="px-5 py-3 font-mono text-fg">
                      <span className="inline-flex items-center gap-1.5">
                        {item.memoCode}
                        <CopyButton
                          value={item.memoCode}
                          variant="icon"
                          showTooltip
                          copiedLabel={t('copied') || 'Copied!'}
                          className="size-5 border-none bg-transparent hover:bg-white/10"
                        />
                      </span>
                    </td>
                    <td className="px-5 py-3 font-mono text-fg">{item.expectedAmount.formatted}</td>
                    <td className="px-5 py-3 font-mono text-fg">{creditAmount(item.expectedCredits, locale)}</td>
                    <td className="px-5 py-3"><Badge tone={item.status === 'paid' ? 'accent' : item.status === 'underpaid' ? 'warn' : 'info'}>{t(`status.${item.status}`)}</Badge></td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </Panel>
    </div>
  );
}
