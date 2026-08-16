'use client';

import { useEffect, useState } from 'react';
import { useLocale, useTranslations } from 'next-intl';
import { api, ApiError } from '@/lib/api/client';
import type { TopupOrderResponse, WalletResponse } from '@/lib/api/types';
import { formatVnd, MIN_TOPUP_VND, TOPUP_STEP_VND } from '@/lib/pricing';
import { money } from '@/lib/format';
import { CheckIcon, CloseIcon } from '@/components/ui/icon';
import { CopyButton } from '@/components/ui/copy-button';
import { Badge } from './ui';

export interface QuickTopupDrawerProps {
  open: boolean;
  onClose: () => void;
  wallet: WalletResponse['wallet'] | null;
  onSuccess?: () => void;
}

type CreditPackage = {
  vnd: number;
  credits: number;
  label: string;
  popular?: boolean;
};

// Cac goi nạp theo Cach 2: 1 Credit = 75 VND (1.5x ROI tu nguon von 50d/Credit)
const CREDIT_PACKAGES_CACH_2: CreditPackage[] = [
  { vnd: 15_000, credits: 200, label: 'Khởi động' },
  { vnd: 75_000, credits: 1_000, label: 'Tiêu chuẩn', popular: true },
  { vnd: 150_000, credits: 2_000, label: 'Developer' },
  { vnd: 375_000, credits: 5_000, label: 'Pro / Team' },
];

export function QuickTopupDrawer({ open, onClose, wallet, onSuccess }: QuickTopupDrawerProps) {
  const t = useTranslations('console.wallet');
  const ts = useTranslations('console.shell');
  const locale = useLocale();

  const [amount, setAmount] = useState<number>(75_000);
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [order, setOrder] = useState<TopupOrderResponse['order'] | null>(null);

  // Dong bang phim Escape
  useEffect(() => {
    if (!open) return;
    function onKeyDown(event: KeyboardEvent) {
      if (event.key === 'Escape') onClose();
    }
    window.addEventListener('keydown', onKeyDown);
    return () => window.removeEventListener('keydown', onKeyDown);
  }, [open, onClose]);

  // Polling don nap moi 3 giay
  useEffect(() => {
    if (!open || !order || order.status === 'paid' || order.status === 'cancelled') return;
    const timer = window.setInterval(async () => {
      try {
        const res = await api.get<TopupOrderResponse>(`/v1/me/topups/${order.id}`);
        setOrder(res.order);
        if (res.order.status === 'paid') {
          onSuccess?.();
        }
      } catch {
        // Polling best-effort
      }
    }, 3000);
    return () => window.clearInterval(timer);
  }, [open, order, onSuccess]);

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

  if (!open) return null;

  const estimatedCredits = Math.round(amount / 75);

  return (
    <div
      role="dialog"
      aria-modal="true"
      aria-labelledby="quick-topup-title"
      className="fixed inset-0 z-50 flex items-center justify-center p-4 sm:p-6"
    >
      {/* Backdrop */}
      <div
        onClick={onClose}
        aria-hidden="true"
        className="fixed inset-0 bg-black/80 backdrop-blur-sm transition-opacity"
      />

      {/* Modal / Drawer Content */}
      <div className="relative z-10 max-h-[90vh] w-full max-w-lg overflow-y-auto rounded-2xl border border-line bg-black/95 p-6 shadow-2xl backdrop-blur-xl animate-[tooltip-spring_0.25s_cubic-bezier(0.34,1.56,0.64,1)_both]">
        {/* Header */}
        <div className="flex items-start justify-between border-b border-line pb-4">
          <div>
            <div className="flex items-center gap-2">
              <span className="size-2 rounded-full bg-accent animate-pulse" />
              <h2 id="quick-topup-title" className="text-lg font-semibold text-fg">
                {ts('quickTopup')}
              </h2>
            </div>
            {wallet ? (
              <p className="mt-1 font-mono text-label text-muted">
                {ts('availableBalance', { amount: money(wallet.available) })}
              </p>
            ) : null}
          </div>
          <button
            type="button"
            onClick={onClose}
            aria-label="Close"
            className="rounded-full border border-line p-1.5 text-muted hover:bg-white/10 hover:text-fg"
          >
            <CloseIcon className="size-4" />
          </button>
        </div>

        {/* Content Body */}
        {!order ? (
          <form onSubmit={createOrder} className="mt-5 space-y-4">
            <div>
              <label className="block text-ui font-medium text-dim">
                Chọn gói nạp Credits (Tỉ giá 1 CR = 75 ₫)
              </label>
              <div className="mt-2.5 grid grid-cols-2 gap-2.5">
                {CREDIT_PACKAGES_CACH_2.map((pkg) => (
                  <button
                    key={pkg.vnd}
                    type="button"
                    onClick={() => setAmount(pkg.vnd)}
                    className={`relative rounded-xl border p-3 text-left transition-all ${
                      amount === pkg.vnd
                        ? 'border-accent bg-accent-soft text-fg ring-1 ring-accent'
                        : 'border-line bg-surface hover:border-accent/40 hover:bg-surface-hover'
                    }`}
                  >
                    {pkg.popular ? (
                      <span className="absolute top-2 right-2 rounded-full bg-accent/20 px-1.5 py-0.5 font-mono text-[10px] font-bold text-accent-light">
                        POPULAR
                      </span>
                    ) : null}
                    <p className="font-mono text-base font-bold text-accent-light">
                      {pkg.credits.toLocaleString('vi-VN')} CR
                    </p>
                    <p className="mt-1 font-mono text-ui text-muted">
                      {formatVnd(pkg.vnd, locale)}
                    </p>
                  </button>
                ))}
              </div>
            </div>

            <div>
              <label htmlFor="custom-topup-amount" className="block text-label text-dim">
                {t('customAmount')}
              </label>
              <input
                id="custom-topup-amount"
                type="number"
                min={MIN_TOPUP_VND}
                max={1000000000}
                step={TOPUP_STEP_VND}
                value={amount}
                onChange={(e) => setAmount(Number(e.target.value))}
                className="mt-1.5 w-full rounded-lg border border-line bg-surface px-4 py-2.5 font-mono text-ui text-fg outline-none focus:border-accent"
              />
            </div>

            {/* Credit Equivalence Readout */}
            <div className="rounded-lg border border-accent/30 bg-accent-soft p-3 font-mono text-ui flex items-center justify-between">
              <span className="text-muted text-label">Nhận vào tài khoản:</span>
              <span className="font-bold text-accent-light">
                💎 {estimatedCredits.toLocaleString('vi-VN')} Credits
              </span>
            </div>

            <p className="rounded-lg border border-warn/30 bg-warn/10 p-3 text-label text-warn leading-relaxed">
              {t('nonRefundable')}
            </p>

            {error ? <p role="alert" className="text-ui text-danger">{error}</p> : null}

            <div className="flex gap-3 pt-2">
              <button
                type="submit"
                disabled={pending || amount < MIN_TOPUP_VND || amount % TOPUP_STEP_VND !== 0}
                className="flex-1 rounded-full bg-fg py-2.5 text-ui font-medium text-bg hover:bg-white/90 disabled:opacity-50"
              >
                {pending ? t('creating') : t('create')}
              </button>
              <button
                type="button"
                onClick={onClose}
                className="rounded-full border border-line px-5 py-2.5 text-ui text-muted hover:bg-white/5 hover:text-fg"
              >
                Đóng
              </button>
            </div>
          </form>
        ) : (
          <div className="mt-5 space-y-4">
            {order.status === 'paid' ? (
              <div className="rounded-xl border border-accent/40 bg-accent-soft p-6 text-center">
                <div className="mx-auto flex size-12 items-center justify-center rounded-full bg-accent text-bg">
                  <CheckIcon className="size-6" />
                </div>
                <h3 className="mt-4 text-lg font-bold text-fg">{t('paidNotice')}</h3>
                <p className="mt-1 font-mono text-ui text-accent-light">
                  +{order.expectedAmount.formatted} (
                  {Math.round(order.expectedAmount.vnd / 75).toLocaleString('vi-VN')} Credits)
                </p>
              </div>
            ) : (
              <div>
                <div className="flex items-center justify-between">
                  <span className="text-ui text-muted">{t('transferTitle')}</span>
                  <Badge tone={order.status === 'underpaid' ? 'warn' : 'info'}>
                    {t(`status.${order.status}`)}
                  </Badge>
                </div>

                <div className="mt-3 divide-y divide-line rounded-xl border border-line bg-surface">
                  <div className="flex items-center justify-between p-3">
                    <span className="text-label text-dim">{t('amount')}</span>
                    <span className="font-mono font-bold text-accent-light">
                      {order.expectedAmount.formatted}
                    </span>
                  </div>
                  <div className="flex items-center justify-between p-3">
                    <span className="text-label text-dim">Quy đổi Credits</span>
                    <span className="font-mono font-bold text-fg">
                      💎 {Math.round(order.expectedAmount.vnd / 75).toLocaleString('vi-VN')} CR
                    </span>
                  </div>
                  <div className="flex items-center justify-between p-3">
                    <span className="text-label text-dim">{t('memo')}</span>
                    <div className="flex items-center gap-2">
                      <span className="font-mono font-bold text-fg">{order.memoCode}</span>
                      <CopyButton value={order.memoCode} variant="icon" showTooltip />
                    </div>
                  </div>
                </div>

                {order.payment?.checkoutUrl ? (
                  <div className="mt-4 text-center">
                    <a
                      href={order.payment.checkoutUrl}
                      target="_blank"
                      rel="noreferrer"
                      className="block w-full rounded-full bg-fg py-2.5 text-center text-ui font-medium text-bg hover:bg-white/90"
                    >
                      {t('openCheckout')}
                    </a>
                  </div>
                ) : null}

                <p className="mt-3 text-center text-micro font-mono text-dim">
                  {t('pollingNotice')}
                </p>
              </div>
            )}
          </div>
        )}
      </div>
    </div>
  );
}
