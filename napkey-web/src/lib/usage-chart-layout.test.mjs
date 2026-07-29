import assert from 'node:assert/strict';
import test from 'node:test';
import { usageChartLayout } from './usage-chart-layout.ts';

test('keeps sparse charts as narrow columns instead of stretching them full width', () => {
  assert.deepEqual(usageChartLayout(1), { sparse: true, columnWidth: 40 });
  assert.deepEqual(usageChartLayout(7), { sparse: true, columnWidth: 40 });
});

test('lets longer time series share the available chart width', () => {
  assert.deepEqual(usageChartLayout(8), { sparse: false, columnWidth: null });
  assert.deepEqual(usageChartLayout(30), { sparse: false, columnWidth: null });
});
