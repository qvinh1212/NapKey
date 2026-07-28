export type CreditMeterTone = 'accent' | 'warn' | 'danger';

export type CreditMeter = {
  used: number;
  balance: number;
  available: number;
  held: number;
  total: number;
  percent: number;
  tone: CreditMeterTone;
};

function safeCredits(value: number): number {
  return Number.isFinite(value) && value > 0 ? value : 0;
}

/** Build a stable customer-facing meter from lifetime usage and the current wallet. */
export function creditMeter(usedValue: number, balanceValue: number, heldValue: number): CreditMeter {
  const used = safeCredits(usedValue);
  const balance = safeCredits(balanceValue);
  const held = Math.min(safeCredits(heldValue), balance);
  const total = used + balance;
  const percent = total === 0 ? 0 : Math.min((used / total) * 100, 100);
  const tone: CreditMeterTone = percent >= 90 ? 'danger' : percent >= 70 ? 'warn' : 'accent';

  return {
    used,
    balance,
    available: balance - held,
    held,
    total,
    percent,
    tone,
  };
}
