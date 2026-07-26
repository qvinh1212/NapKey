'use client';

import { useCallback, useEffect, useState } from 'react';
import { api } from '@/lib/api/client';
import type { OperationsAlert, OperationsSummary } from '@/lib/api/types';
import { useSession } from './session-provider';
import { Badge, Panel, PanelHeader, StatCard } from './ui';

export function OperationsDashboard() {
  const session = useSession();
  const [data, setData] = useState<OperationsSummary | null>(null);
  const [error, setError] = useState('');
  const [reconciling, setReconciling] = useState(false);
  const [alerts, setAlerts] = useState<OperationsAlert[]>([]);

  const load = useCallback(async () => {
    try {
		const [summary, alertResponse] = await Promise.all([
			api.get<OperationsSummary>('/v1/admin/operations/summary?days=30'),
			api.get<{ alerts: OperationsAlert[] }>('/v1/admin/operations/alerts'),
		]);
		setData(summary);
		setAlerts(alertResponse.alerts);
      setError('');
    } catch {
      setError('Không tải được trạng thái vận hành.');
    }
  }, []);

  useEffect(() => {
    if (session.status !== 'authenticated' || !session.permissions.includes('operations.read')) return;
	const initial = window.setTimeout(() => void load(), 0);
    const timer = window.setInterval(() => void load(), 30_000);
	return () => {
		window.clearTimeout(initial);
		window.clearInterval(timer);
	};
  }, [load, session]);

  if (session.status !== 'authenticated' || !session.permissions.includes('operations.read')) {
    return <Panel className="p-6 text-sm text-danger">Bạn không có quyền xem khu vực vận hành.</Panel>;
  }
  if (!data && !error) return <p className="text-ui text-dim">Đang tải số liệu vận hành...</p>;
  if (!data) return <Panel className="p-6 text-sm text-danger">{error}</Panel>;

  const unhealthy = data.wallets.driftCount + data.payments.stuck + data.keySync.failed + data.holds.expired + (data.dataPlane.healthy ? 0 : 1);
  async function reconcile() {
    setReconciling(true);
    try {
      await api.post('/v1/admin/operations/reconcile-wallets');
      await load();
    } finally {
      setReconciling(false);
    }
  }

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-end justify-between gap-4">
        <div>
          <p className="font-mono text-label tracking-[0.18em] text-accent uppercase">Stage 5 / Operations</p>
          <h2 className="mt-2 text-3xl tracking-[-0.03em]">Trung tâm vận hành</h2>
          <p className="mt-2 text-ui text-dim">Tự làm mới mỗi 30 giây · cửa sổ doanh thu 30 ngày</p>
        </div>
        <Badge tone={unhealthy > 0 ? 'danger' : 'accent'}>{unhealthy > 0 ? `${unhealthy} điểm cần xử lý` : 'Hệ thống ổn định'}</Badge>
      </div>

      <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
        <StatCard label="Doanh thu" value={data.revenue.formatted} hint="Usage đã ghi nhận" />
        <StatCard label="Chi phí upstream" value={data.upstreamCostEstimate.formatted} hint="Ước tính từ giá bán / 1,30" />
        <StatCard label="Biên gộp" value={data.margin.formatted} tone="accent" hint="Doanh thu trừ chi phí ước tính" />
        <StatCard label="Cảnh báo mở" value={String(data.openAlerts)} tone={data.openAlerts ? 'danger' : 'default'} />
      </div>

      <div className="grid gap-4 lg:grid-cols-2">
		<Panel>
			<PanelHeader title="Sức khỏe data plane" description="Pool Kiro và đường báo usage về control plane" />
			<div className="grid grid-cols-2 gap-y-5 p-5 text-ui sm:grid-cols-3">
				<Metric label="Tài khoản khả dụng" value={data.dataPlane.available ?? 0} danger />
				<Metric label="Tổng tài khoản" value={data.dataPlane.accounts ?? 0} />
				<Metric label="Request lỗi" value={data.dataPlane.failedRequests ?? 0} danger />
				<Metric label="Usage chờ gửi" value={data.dataPlane.usageReporting?.pending ?? 0} />
				<Metric label="Usage bị mất" value={data.dataPlane.usageReporting?.dropped ?? 0} danger />
				<Metric label="Usage đã gửi" value={data.dataPlane.usageReporting?.sent ?? 0} />
			</div>
			{data.dataPlane.error ? <p className="border-t border-line px-5 py-3 text-ui text-danger">{data.dataPlane.error}</p> : null}
		</Panel>
        <Panel>
          <PanelHeader title="Toàn vẹn ví" description="Đối chiếu balance cache với ledger append-only" action={
            session.permissions.includes('billing.reconcile') ? <button className="rounded-md border border-line px-3 py-2 text-ui hover:bg-surface-hover disabled:opacity-50" disabled={reconciling} onClick={() => void reconcile()}>{reconciling ? 'Đang đối chiếu...' : 'Đối chiếu ngay'}</button> : null
          } />
          <div className="grid grid-cols-2 gap-4 p-5 text-ui">
            <div><p className="text-dim">Ví lệch</p><p className="mt-1 text-xl tabular-nums">{data.wallets.driftCount}</p></div>
            <div><p className="text-dim">Tổng độ lệch</p><p className="mt-1 text-xl tabular-nums">{data.wallets.absoluteDrift.formatted}</p></div>
          </div>
        </Panel>
        <Panel>
          <PanelHeader title="Hàng đợi & đồng bộ" description="Các tác vụ có nguy cơ bị kẹt" />
          <div className="grid grid-cols-2 gap-y-5 p-5 text-ui sm:grid-cols-3">
            <Metric label="Payment chưa khớp" value={data.payments.unmatched} />
            <Metric label="Payment bị kẹt" value={data.payments.stuck} danger />
            <Metric label="Payment từ chối" value={data.payments.rejected} />
            <Metric label="Key chờ sync" value={data.keySync.pending} />
            <Metric label="Key sync lỗi" value={data.keySync.failed} danger />
            <Metric label="Hold quá hạn" value={data.holds.expired} danger />
          </div>
        </Panel>
      </div>
      {error ? <p role="status" className="text-ui text-warn">{error}</p> : null}
		<Panel>
			<PanelHeader title="Cảnh báo đang mở" description="Tự khử trùng lặp theo fingerprint và tự đóng khi điều kiện hồi phục" />
			{alerts.length === 0 ? <p className="p-5 text-ui text-dim">Không có cảnh báo đang mở.</p> : <div className="divide-y divide-line">{alerts.map((alert) => (
				<div key={alert.id} className="flex flex-wrap items-center justify-between gap-3 px-5 py-4 text-ui">
					<div><p className="text-fg">{alert.title}</p><p className="mt-1 font-mono text-label text-dim">{alert.type}</p></div>
					<Badge tone={alert.severity === 'critical' ? 'danger' : alert.severity === 'warning' ? 'warn' : 'info'}>{alert.severity}</Badge>
				</div>
			))}</div>}
		</Panel>
    </div>
  );
}

function Metric({ label, value, danger = false }: { label: string; value: number; danger?: boolean }) {
  return <div><p className="text-dim">{label}</p><p className={`mt-1 text-xl tabular-nums ${danger && value > 0 ? 'text-danger' : 'text-fg'}`}>{value}</p></div>;
}
