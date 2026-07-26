'use client';

import { useEffect, useState } from 'react';
import { useLocale, useTranslations } from 'next-intl';
import { ButtonLink } from '@/components/ui/button';
import { api, ApiError, rangeQuery } from '@/lib/api/client';
import type { UsageDetailResponse, UsageSummaryResponse } from '@/lib/api/types';
import { billingRange, compact, count, money } from '@/lib/format';
import { UsageChart } from './usage-chart';
import { Badge, ErrorNotice, Panel, PanelHeader, StatCard, Td, Th, TableScroll } from './ui';

/**
 * Trang tong quan console.
 *
 * Hai request song song: tong hop (`/v1/me/usage`) va chi tiet 30 ngay
 * (`/v1/me/usage/detail`). Goi song song vi chung khong phu thuoc nhau - goi tuan tu
 * se lam thoi gian cho bang tong hai chang mang.
 */

type State =
  | { status: 'loading' }
  | { status: 'error'; message: string }
  | { status: 'ready'; summary: UsageSummaryResponse; detail: UsageDetailResponse };

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
        // Song song: hai endpoint khong phu thuoc nhau, goi tuan tu se lam thoi gian
        // cho bang tong hai chang mang.
        const [summary, detail] = await Promise.all([
          api.get<UsageSummaryResponse>('/v1/me/usage', controller.signal),
          api.get<UsageDetailResponse>(`/v1/me/usage/detail${query}`, controller.signal),
        ]);
        setState({ status: 'ready', summary, detail });
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
    return (
      <div role="status" className="py-24 text-center text-ui text-dim">
        {t('loading')}
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

  const { summary, detail } = state;
  const { last30Days } = summary;

  return (
    <div className="flex flex-col gap-6">
      <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
        <StatCard
          label={t('stats.cost30d')}
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
                  <Td align="right" className="text-accent-light">
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