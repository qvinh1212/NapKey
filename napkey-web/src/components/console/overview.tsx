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
  const [reloadToken, setReloadToken] = useState(0);

  useEffect(() => {
    const controller = new AbortController();

    async function run() {
      setState({ status: 'loading' });
      try {
        const query = rangeQuery(billingRange(30));
        const [summary, detail, walletResponse] = await Promise.all([
          api.get<UsageSummaryResponse>('/v1/me/usage', controller.signal),
          api.get<UsageDetailResponse>(`/v1/me/usage/detail${query}`, controller.signal),
          api.get<WalletResponse>('/v1/me/wallet', controller.signal).catch(() => null),
        ]);
        setState({ status: 'ready', summary, detail, wallet: walletResponse?.wallet ?? null });
      } catch (error) {
        if (controller.signal.aborted) return;
        const message = error instanceof ApiError ? error.message : t('loadFailed');
        setState({ status: 'error', message });
      }
    }

    void run();
    return () => controller.abort();
  }, [t, reloadToken]);

  if (state.status === 'loading') {
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
  const progressPercent = Math.round((activation.completedSteps / 3) * 100);

  return (
    <div className="flex flex-col gap-6">
      {activation.stage !== 'activated' ? (
        <section className="relative overflow-hidden rounded-2xl border border-accent/40 bg-[radial-gradient(circle_at_top_right,var(--color-accent-soft),transparent_55%),var(--color-surface)] p-6 sm:p-7 shadow-[0_20px_60px_rgba(0,0,0,0.4)]">
          <div className="relative z-10 grid gap-6 lg:grid-cols-[minmax(0,1fr)_auto] lg:items-center">
            <div>
              <div className="flex flex-wrap items-center gap-3">
                <Badge tone="accent">{t('activation.badge')}</Badge>
                {wallet ? (
                  <span className="font-mono text-label tracking-[0.08em] text-accent-light uppercase">
                    {t('activation.balance', { amount: money(wallet.available) })}
                  </span>
                ) : null}
                <span className="font-mono text-micro text-dim">
                  Tiến độ: {activation.completedSteps}/3 bước ({progressPercent}%)
                </span>
              </div>

              {/* Visual Progress Bar */}
              <div className="mt-3.5 h-1.5 w-full max-w-lg overflow-hidden rounded-full bg-white/10">
                <div
                  className="h-full bg-accent transition-all duration-500 ease-out"
                  style={{ width: `${progressPercent}%` }}
                />
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
                      <span
                        className={`flex size-6 items-center justify-center rounded-full border font-mono text-[11px] transition-all ${
                          complete
                            ? 'border-accent bg-accent text-bg font-bold'
                            : current
                              ? 'border-accent text-accent-light ring-2 ring-accent/30'
                              : 'border-line text-dim'
                        }`}
                      >
                        {complete ? <CheckIcon className="size-3.5" /> : index + 1}
                      </span>
                      <span className={complete ? 'text-fg line-through opacity-80' : current ? 'text-accent-light font-medium' : 'text-dim'}>
                        {t(`activation.steps.${step}`)}
                      </span>
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
          hint={t('stats.activeKeysHint', { total: money(summary.usage.totalCost) })}
        />
      </div>

      <Panel as="section">
        <PanelHeader title={t('chartTitle')} description={t('chartDescription')} />
        <UsageChart daily={detail.daily} />
      </Panel>

      <Panel as="section">
        <PanelHeader
          title={t('byModelTitle')}
          description={t('byModelDescription')}
          action={
            <ButtonLink href="/console/usage" variant="pill">
              {t('viewLedger')}
            </ButtonLink>
          }
        />

        {detail.byModel.length === 0 ? (
          <div className="p-8 text-center text-ui text-dim">{t('byModelEmpty')}</div>
        ) : (
          <TableScroll>
            <thead>
              <tr>
                <Th>{tu('colModel')}</Th>
                <Th align="right">{tu('colRequests')}</Th>
                <Th align="right">{tu('colTokens')}</Th>
                <Th align="right">{tu('colCost')}</Th>
              </tr>
            </thead>
            <tbody className="divide-y divide-line">
              {detail.byModel.map((item) => (
                <tr key={item.model} className="transition-colors hover:bg-surface-hover">
                  <Td className="font-mono text-fg">{item.model}</Td>
                  <Td align="right" className="font-mono text-dim tabular-nums">
                    {count(item.requests, locale)}
                  </Td>
                  <Td align="right" className="font-mono text-dim tabular-nums">
                    {compact(item.tokens.total, locale)}
                  </Td>
                  <Td align="right" className="font-mono font-medium text-fg tabular-nums">
                    {money(item.cost)}
                  </Td>
                </tr>
              ))}
            </tbody>
          </TableScroll>
        )}
      </Panel>
    </div>
  );
}
