import { getTranslations } from 'next-intl/server';
import { Link } from '@/i18n/navigation';
import { readPublicStatus } from '@/lib/public-status';
import { readModelCatalog } from '@/lib/model-catalog';
import { ArrowUpRightIcon } from '@/components/ui/icon';

const dotForStatus = {
  operational: 'bg-accent shadow-[0_0_12px_rgba(16,185,129,0.65)]',
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

/**
 * Bang chung van hanh ngay duoi hero.
 *
 * Landing page truoc day khong he dan mot so do nao, du /status va /v1/models da
 * co san du lieu. Voi developer, mot dai tin hieu that thuyet phuc hon moi cau
 * copy, nen ba o nay doc tu chinh nguon ma trang /status dung.
 *
 * Ca hai nguon deu tu suy ra fallback khi upstream im lang, nen khoi nay khong
 * bao gio lam vo trang chu.
 */
export async function LiveSignals() {
  const [t, status, catalog] = await Promise.all([
    getTranslations('signals'),
    readPublicStatus({ revalidateSeconds: 60 }),
    readModelCatalog(),
  ]);

  // `normalizePublicStatus` tra ve outage khi khong doc duoc gi, dung cho trang
  // /status nhung sai cho o day: mot landing page bao "dang gian doan" chi vi
  // build host khong voi tay den napkey-core la tu bao mot su co khong ton tai.
  // `checkedAt` rong la dau hieu duy nhat phan biet hai truong hop.
  const statusKey = status.checkedAt === '' ? 'unknown' : status.status;

  return (
    <section aria-labelledby="signals-heading" className="border-y border-line bg-surface">
      <h2 id="signals-heading" className="sr-only">
        {t('title')}
      </h2>
      <div className="container-page">
        <dl className="grid gap-px sm:grid-cols-3">
          <div className="py-6 sm:pr-8">
            <dt className="font-mono text-label tracking-[0.16em] text-dim uppercase">
              {t('gatewayLabel')}
            </dt>
            <dd className={`mt-3 flex items-center gap-2.5 font-mono text-lg ${textForStatus[statusKey]}`}>
              <span aria-hidden className={`size-2 shrink-0 rounded-full ${dotForStatus[statusKey]}`} />
              {t(`states.${statusKey}`)}
            </dd>
          </div>

          <div className="border-t border-line py-6 sm:border-t-0 sm:border-l sm:px-8">
            <dt className="font-mono text-label tracking-[0.16em] text-dim uppercase">
              {t('modelsLabel')}
            </dt>
            <dd className="mt-3 font-mono text-lg text-fg tabular-nums">
              {catalog.live ? t('modelsValue', { count: catalog.models.length }) : t('modelsUnknown')}
            </dd>
          </div>

          <div className="border-t border-line py-6 sm:border-t-0 sm:border-l sm:pl-8">
            <dt className="font-mono text-label tracking-[0.16em] text-dim uppercase">
              {t('tieredLabel')}
            </dt>
            <dd className="mt-3 font-mono text-lg text-fg tabular-nums">
              {t('tieredValue')}
            </dd>
          </div>
        </dl>

        <p className="border-t border-line py-4 text-ui text-dim">
          <Link
            href="/status"
            className="inline-flex items-center gap-2 transition-colors hover:text-fg"
          >
            {t('link')}
            <ArrowUpRightIcon className="size-3.5" />
          </Link>
        </p>
      </div>
    </section>
  );
}
