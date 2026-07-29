export function usageBarPercent(micros: number, maxMicros: number) {
  if (!Number.isFinite(micros) || micros <= 0) return 0;
  if (!Number.isFinite(maxMicros) || maxMicros <= 0) return 0;
  return Math.min(Math.max((micros / maxMicros) * 100, 2), 100);
}
