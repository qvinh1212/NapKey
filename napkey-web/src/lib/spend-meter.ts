export type SpendMeterTone = 'accent' | 'warn' | 'danger';

export type SpendMeter = {
  /** All amounts are integer micro-VND, the same unit the backend settles in. */
  used: number;
  balance: number;
  available: number;
  held: number;
  total: number;
  percent: number;
  tone: SpendMeterTone;
};

function safeAmount(value: number): number {
  return Number.isFinite(value) && value > 0 ? value : 0;
}

/**
 * Build the customer-facing spend meter from lifetime usage and the current wallet.
 *
 * Measured in money, not credits. This used to take credit counts, and every request
 * the 9Router upstream serves reports zero credits -- it speaks the OpenAI protocol and
 * has no credit meter, so settlement prices from tokens instead. The bar therefore sat
 * at 0% while a wallet drained, and the top-up prompt at 70% never fired. Money is what
 * was actually debited, so it is what the meter measures.
 */
export function spendMeter(usedValue: number, balanceValue: number, heldValue: number): SpendMeter {
  const used = safeAmount(usedValue);
  const balance = safeAmount(balanceValue);
  const held = Math.min(safeAmount(heldValue), balance);
  const total = used + balance;
  const percent = total === 0 ? 0 : Math.min((used / total) * 100, 100);
  const tone: SpendMeterTone = percent >= 90 ? 'danger' : percent >= 70 ? 'warn' : 'accent';

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
