export function usageChartLayout(dayCount: number) {
  const sparse = dayCount <= 7;
  return { sparse, columnWidth: sparse ? 40 : null };
}

