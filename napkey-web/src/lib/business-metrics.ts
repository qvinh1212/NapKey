type BusinessFunnel = {
  newUsers: number;
  verifiedUsers: number;
  activatedUsers: number;
  newPayingUsers: number;
  payingCustomers: number;
  repeatCustomers: number;
};

function percent(value: number, total: number) {
  if (!Number.isFinite(value) || !Number.isFinite(total) || value <= 0 || total <= 0) return 0;
  return Math.round((value / total) * 1000) / 10;
}

export function businessRates(value: BusinessFunnel) {
  return {
    verification: percent(value.verifiedUsers, value.newUsers),
    activation: percent(value.activatedUsers, value.newUsers),
    payment: percent(value.newPayingUsers, value.newUsers),
    repeat: percent(value.repeatCustomers, value.payingCustomers),
  };
}
