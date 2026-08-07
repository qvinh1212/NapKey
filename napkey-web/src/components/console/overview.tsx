'use client';

import { useEffect, useState } from 'react';
import { useLocale, useTranslations } from 'next-intl';
import { ButtonLink } from '@/components/ui/button';
import { ArrowRightIcon, CheckIcon } from '@/components/ui/icon';
import { api, ApiError, rangeQuery } from '@/lib/api/client';
import type { UsageDetailResponse, UsageSummaryResponse, WalletResponse } from '@/lib/api/types';
import { activationState } from '@/lib/activation';
import { billingRange, compact, count, money } from '@/lib/format';
import { UsageChart } from './usage-chart';
import { SpendMeter } from './spend-meter';
import {
  Badge,
  ErrorNotice,
  LoadingStatus,
  Panel,
  PanelHeader,
  SkeletonCards,
  SkeletonPanel,
  StatCard,
  Td,
  Th,
  TableScroll,
} from './ui';

/**
 * Trang tong quan console.
 *
 * Usage summary, 30-day detail, and wallet are loaded in parallel. The wallet is
 * optional so a transient billing read failure does not hide the usage ledger.
 */

type State =
  | { status: 'loading' }
  | { status: 'error'; message: string }
  | {
      status: 'ready';
      summary: UsageSummaryResponse;
      detail: UsageDetailResponse;
      wallet: WalletResponse['wallet'] | null;
    };

export function Overview() {
  const t = useTranslations('console.overview');
  const tu = useTranslations('console.usage');
  const locale = useLocale();
  const [state, setState] = useState<State>({ status: 'loading' });
  // Tang len de yeu cau doc lai. Re hon viec giu mot ham `load` trong dependency.
  const [reloadToken, setReloadToken] = useState(0);

  useEffect(() => {
    const controller = new AbortController();

    async function run() {
      setState({ status: 'loading' });
      try {
        const query = rangeQuery(billingRange(30));
        // Wallet failure only hides the meter; usage remains useful on its own.
        const [summary, detail, walletResponse] = await Promise.all([
          api.get<UsageSummaryResponse>('/v1/me/usage', controller.signal),
          api.get<UsageDetailResponse>(`/v1/me/usage/detail${query}`, controller.signal),
          api.get<WalletResponse>('/v1/me/wallet', controller.signal).catch(() => null),
        ]);
        setState({ status: 'ready', summary, detail, wallet: walletResponse?.wallet ?? null });
      } catch (error) {
        // Huy do unmount hoac do doc lai thi khong phai loi de bao cho nguoi dung.
        if (controller.signal.aborted) return;
        const message = error instanceof ApiError ? error.message : t('loadFailed');
        setState({ status: 'error', message });
      }
    }

    void run();
    return () => controller.abort();
  }, [t, reloadToken]);

  if (state.status === 'loading') {
    // Khung xuong dung theo bo cuc that cua trang (meter, hang the, hai bang) de
    // noi dung khong nhay cho khi so lieu ve.
    return (
      <div className="flex flex-col gap-6">
        <LoadingStatus label={t('loading')} />
        <SkeletonPanel rows={2} />
        <SkeletonCards />
        <SkeletonPanel rows={6} />
        <SkeletonPanel rows={4} />
      </div>
    );
  }

  if (state.status === 'error') {
    return (
      <ErrorNotice
        message={state.message}
        onRetry={() => setReloadToken((v) => v + 1)}
        retryLabel={t('retry')}
      />
    );
  }

  const { summary, detail, wallet } = state;
  const { last30Days } = summary;
  const activation = activationState({
    activeKeys: summary.usage.activeKeys,
    totalRequests: summary.usage.totalRequests,
  });
  const activationTarget = activation.stage === 'create_key' ? '/console/keys' : '/console/developer';

  return (
    <div className="flex flex-col gap-6">
      {activation.stage !== 'activated' ? (
        <section className="relative overflow-hidden rounded-xl border border-accent/40 bg-[radial-gradient(circle_at_top_right,var(--color-accent-soft),transparent_55%),var(--color-surface)] px-5 py-6 sm:px-7 sm:py-7">
          <div className="relative z-10 grid gap-6 lg:grid-cols-[minmax(0,1fr)_auto] lg:items-center">
            <div>
              <div className="flex flex-wrap items-center gap-3">
                <Badge tone="accent">{t('activation.badge')}</Badge>
                {wallet ? (
                  <span className="font-mono text-label tracking-[0.08em] text-accent-light uppercase">
                    {t('activation.balance', { amount: money(wallet.available) })}
                  </span>
                ) : null}
              </div>
              <h2 className="mt-4 max-w-2xl text-2xl tracking-[-0.03em] text-fg sm:text-3xl">
                {t(`activation.${activation.stage}.title`)}
              </h2>
              <p className="mt-2 max-w-2xl text-ui leading-relaxed text-muted">
                {t(`activation.${activation.stage}.description`)}
              </p>
              <ol className="mt-5 flex flex-wrap gap-x-5 gap-y-3" aria-label={t('activation.progressLabel')}>
                {(['account', 'key', 'request'] as const).map((step, index) => {
                  const complete = index < activation.completedSteps;
                  const current = index === activation.completedSteps;
                  return (
                    <li key={step} className="flex items-center gap-2 text-ui">
                      <span className={`flex size-6 items-center justify-center rounded-full border font-mono text-[11px] ${complete ? 'border-accent bg-accent text-bg' : current ? 'border-accent text-accent-light' : 'border-line text-dim'}`}>
                        {complete ? <CheckIcon className="size-3.5" /> : index + 1}
                      </span>
                      <span className={complete || current ? 'text-fg' : 'text-dim'}>{t(`activation.steps.${step}`)}</span>
                    </li>
                  );
                })}
              </ol>
            </div>
            <ButtonLink href={activationTarget} className="w-full lg:w-auto">
              {t(`activation.${activation.stage}.cta`)}
              <ArrowRightIcon />
            </ButtonLink>
          </div>
        </section>
      ) : null}

      {wallet ? <SpendMeter usedAmount={summary.usage.totalCost} wallet={wallet} /> : null}

      <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
        <StatCard
          label={t('stats.spend30d')}
          value={money(last30Days.cost)}
          hint={t('stats.cost30dHint')}
          tone="accent"
        />
        <StatCard
          label={t('stats.requests30d')}
          value={count(last30Days.requests, locale)}
          hint={
            last30Days.errorRequests > 0
              ? t('stats.requestsHintErrors', {
                  count: count(last30Days.errorRequests, locale),
                })
              : t('stats.requestsHintClean')
          }
        />
        <StatCard
          label={t('stats.tokens30d')}
          value={compact(last30Days.tokens.total, locale)}
          hint={t('stats.tokensHint', {
            input: compact(last30Days.tokens.input, locale),
            output: compact(last30Days.tokens.output, locale),
          })}
        />
        <StatCard
          label={t('stats.activeKeys')}
          value={count(summary.usage.activeKeys, locale)}
          hint={t('stats.activeKeysHint', {
            total: money(summary.usage.totalCost),
          })}
        />
      </div>

      {/*
        Hai canh bao nay la ly do trang nay ton tai. Chung noi cho khach biet con so
        tien ben tren duoc tao ra the nao, chu khong bat ho tin.
      */}
      {last30Days.estimatedRequests > 0 ? (
        <div className="flex flex-wrap items-center gap-3 rounded-lg border border-line bg-surface px-5 py-4">
          <Badge tone="info">{t('estimatedBadge')}</Badge>
          <p className="text-ui text-muted">
            {t('estimatedNotice', {
              count: count(last30Days.estimatedRequests, locale),
              total: count(last30Days.requests, locale),
            })}
          </p>
        </div>
      ) : null}

      {last30Days.unpricedRequests > 0 ? (
        <div
          role="status"
          className="flex flex-wrap items-center gap-3 rounded-lg border border-warn/30 bg-warn/10 px-5 py-4"
        >
          <Badge tone="warn">{t('unpricedBadge')}</Badge>
          <p className="text-ui text-warn">
            {t('unpricedNotice', { count: count(last30Days.unpricedRequests, locale) })}
          </p>
        </div>
      ) : null}

      <Panel as="section">
        <PanelHeader title={t('chartTitle')} description={t('chartDescription')} />
        <UsageChart daily={detail.daily} />
      </Panel>

      <Panel as="section">
        <PanelHeader title={t('byModelTitle')} description={t('byModelDescription')} />
        {detail.byModel.length === 0 ? (
          <p className="px-5 py-12 text-center text-ui text-dim">{t('byModelEmpty')}</p>
        ) : (
          <TableScroll>
            <thead>
              <tr>
                <Th>{tu('colModel')}</Th>
                <Th align="right">{tu('colRequests')}</Th>
                <Th align="right">{tu('colInput')}</Th>
                <Th align="right">{tu('colOutput')}</Th>
                <Th align="right">{tu('colCacheRead')}</Th>
                <Th align="right">{tu('colCost')}</Th>
              </tr>
            </thead>
            <tbody className="divide-y divide-line">
              {detail.byModel.map((row) => (
                <tr key={row.model} className="transition-colors hover:bg-surface-hover">
                  <Td className="font-mono text-ui text-muted">{row.model}</Td>
                  <Td align="right">{count(row.requests, locale)}</Td>
                  <Td align="right">{compact(row.tokens.input, locale)}</Td>
                  <Td align="right">{compact(row.tokens.output, locale)}</Td>
                  <Td align="right">{compact(row.tokens.cacheRead, locale)}</Td>
                  <Td align="right" className="font-mono text-accent-light">
                    {money(row.cost)}
                  </Td>
                </tr>
              ))}
            </tbody>
          </TableScroll>
        )}
      </Panel>

      {/*
        Trang thai tinh tien lay tu backend chu khong hardcode: Giai doan 3 do usage
        nhung chua thu tien, va console khong duoc tu bay ra mot so du chua ton tai.
      */}
      <Panel as="section" className="px-5 py-4">
        <div className="flex flex-wrap items-center gap-3">
          <Badge tone={summary.billing.mode === 'metered_no_wallet' ? 'info' : 'neutral'}>
            {t(`billingMode.${summary.billing.mode}`)}
          </Badge>
          <p className="text-ui text-dim">{summary.billing.message}</p>
        </div>
        <div className="mt-4">
          <ButtonLink href="/console/usage" variant="pill">
            {t('viewLedger')}
          </ButtonLink>
        </div>
      </Panel>
    </div>
  );
}
