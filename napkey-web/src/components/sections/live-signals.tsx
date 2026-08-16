import { getTranslations } from 'next-intl/server';
import { Link } from '@/i18n/navigation';
import { readPublicStatus } from '@/lib/public-status';
import { readModelCatalog } from '@/lib/model-catalog';
import { ArrowUpRightIcon } from '@/components/ui/icon';

const dotForStatus = {
  operational: 'bg-accent shadow-[0_0_12px_rgba(0,134,255,0.65)] animate-pulse',
  degraded: 'bg-warn shadow-[0_0_12px_rgba(250,204,21,0.55)]',
  outage: 'bg-danger shadow-[0_0_12px_rgba(239,35,60,0.55)]',
  unknown: 'bg-dim',
} as const;

const textForStatus = {
  operational: 'text-accent-light',
  degraded: 'text-warn',
  outage: 'text-danger',
  unknown: 'text-muted',
} as const;

export async function LiveSignals() {
  const [t, status, catalog] = await Promise.all([
    getTranslations('signals'),
    readPublicStatus({ revalidateSeconds: 60 }),
    readModelCatalog(),
  ]);

  const statusKey = status.checkedAt === '' ? 'unknown' : status.status;

  return (
    <section aria-labelledby="signals-heading" className="border-y border-line bg-surface/80 backdrop-blur-sm">
      <h2 id="signals-heading" className="sr-only">
        {t('title')}
      </h2>
      <div className="container-page">
        <dl className="grid gap-px sm:grid-cols-2 lg:grid-cols-5">
          {/* Metric 1: Gateway Status */}
          <div className="py-5 sm:pr-6">
            <dt className="font-mono text-micro tracking-[0.14em] text-dim uppercase">
              {t('gatewayLabel')}
            </dt>
            <dd className={`mt-2.5 flex items-center gap-2 font-mono text-ui font-semibold ${textForStatus[statusKey]}`}>
              <span aria-hidden className={`size-2 shrink-0 rounded-full ${dotForStatus[statusKey]}`} />
              <span className="truncate">{t(`states.${statusKey}`)}</span>
            </dd>
          </div>

          {/* Metric 2: TTFT Latency */}
          <div className="border-t border-line py-5 sm:border-t-0 sm:border-l sm:px-6">
            <dt className="font-mono text-micro tracking-[0.14em] text-dim uppercase">
              {t('latencyLabel')}
            </dt>
            <dd className="mt-2.5 flex items-center gap-1.5 font-mono text-ui font-semibold text-accent-light tabular-nums">
              <span>{t('latencyValue')}</span>
            </dd>
          </div>

          {/* Metric 3: Streaming Throughput */}
          <div className="border-t border-line py-5 sm:border-t-0 lg:border-l sm:px-6">
            <dt className="font-mono text-micro tracking-[0.14em] text-dim uppercase">
              {t('throughputLabel')}
            </dt>
            <dd className="mt-2.5 font-mono text-ui font-semibold text-fg tabular-nums">
              {t('throughputValue')}
            </dd>
          </div>

          {/* Metric 4: Verified Models Serving */}
          <div className="border-t border-line py-5 sm:border-t-0 sm:border-l sm:px-6">
            <dt className="font-mono text-micro tracking-[0.14em] text-dim uppercase">
              {t('modelsLabel')}
            </dt>
            <dd className="mt-2.5 font-mono text-ui font-semibold text-fg tabular-nums">
              {catalog.live ? t('modelsValue', { count: catalog.models.length }) : t('modelsUnknown')}
            </dd>
          </div>

          {/* Metric 5: SLA Commitment */}
          <div className="border-t border-line py-5 sm:border-t-0 sm:border-l sm:pl-6">
            <dt className="font-mono text-micro tracking-[0.14em] text-dim uppercase">
              {t('slaLabel')}
            </dt>
            <dd className="mt-2.5 font-mono text-ui font-semibold text-fg tabular-nums">
              {t('slaValue')}
            </dd>
          </div>
        </dl>

        <div className="flex items-center justify-between border-t border-line/60 py-3 text-micro text-dim font-mono">
          <span>Hạ tầng Edge Gateway TP.HCM & Singapore · Liveness Probe hoạt động 24/7</span>
          <Link
            href="/status"
            className="inline-flex items-center gap-1.5 text-accent-light hover:underline"
          >
            {t('link')}
            <ArrowUpRightIcon className="size-3" />
          </Link>
        </div>
      </div>
    </section>
  );
}
