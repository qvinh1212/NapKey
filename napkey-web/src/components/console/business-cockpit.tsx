import type { BusinessSummary } from '@/lib/api/types';
import { businessRates } from '@/lib/business-metrics';
import { Badge, Panel, PanelHeader, StatCard } from './ui';

export function BusinessCockpit({ data }: { data: BusinessSummary }) {
  const rates = businessRates({
    ...data.funnel,
    payingCustomers: data.customers.paying,
    repeatCustomers: data.customers.repeat,
  });
  const funnel = [
    { label: 'Tạo tài khoản', value: data.funnel.newUsers, rate: 100 },
    { label: 'Xác minh email', value: data.funnel.verifiedUsers, rate: rates.verification },
    { label: 'Gọi API thành công', value: data.funnel.activatedUsers, rate: rates.activation },
    { label: 'Trở thành khách trả tiền', value: data.funnel.newPayingUsers, rate: rates.payment },
  ];

  return (
    <section className="space-y-4" aria-labelledby="business-heading">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <p className="font-mono text-label tracking-[0.18em] text-accent uppercase">Growth / {data.windowDays} ngày</p>
          <h2 id="business-heading" className="mt-2 text-2xl tracking-[-0.025em]">Business cockpit</h2>
          <p className="mt-1 text-ui text-dim">Tiền nạp thực nhận và hành trình từ đăng ký đến khách hàng trả tiền.</p>
        </div>
        <Badge tone="neutral">Không dùng tracking bên thứ ba</Badge>
      </div>

      <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
        <StatCard label="Tiền nạp đã nhận" value={data.payments.cashCollected.formatted} hint={`${data.payments.paidOrders} lệnh đã thanh toán`} tone="accent" />
        <StatCard label="Giá trị đơn trung bình" value={data.payments.averageOrder.formatted} hint="Tổng tiền nạp / số lệnh đã trả" />
        <StatCard label="Khách nạp tiền" value={String(data.customers.paying)} hint={`${data.customers.repeat} khách nạp lại · ${rates.repeat}%`} />
        <StatCard label="Nghĩa vụ số dư ví" value={data.walletLiability.formatted} hint="Credit đã bán nhưng khách chưa sử dụng" tone="warn" />
      </div>

      <Panel>
        <PanelHeader title="Phễu chuyển đổi cohort" description={`Các tài khoản được tạo trong ${data.windowDays} ngày gần nhất; activation và payment chỉ tính cùng cohort.`} />
        <div className="grid gap-5 p-5 lg:grid-cols-4">
          {funnel.map((step, index) => (
            <div key={step.label} className="min-w-0">
              <div className="flex items-baseline justify-between gap-3"><p className="text-ui text-muted">{step.label}</p><span className="font-mono text-label text-dim">0{index + 1}</span></div>
              <p className="mt-2 text-2xl tabular-nums text-fg">{step.value}</p>
              <div className="mt-3 h-1.5 overflow-hidden rounded-full bg-white/5" aria-hidden><div className="h-full rounded-full bg-accent" style={{ width: `${Math.max(0, Math.min(step.rate, 100))}%` }} /></div>
              <p className="mt-2 font-mono text-label text-dim">{step.rate}% trên tài khoản mới</p>
            </div>
          ))}
        </div>
      </Panel>
    </section>
  );
}
