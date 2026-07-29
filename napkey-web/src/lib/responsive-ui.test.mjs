import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';

function source(path) {
  return readFileSync(new URL(path, import.meta.url), 'utf8');
}

test('uses compact mobile gutters and restores desktop section rhythm', () => {
  const css = source('../app/globals.css');
  assert.match(css, /padding-inline: 1rem/);
  assert.match(css, /@media \(min-width: 40rem\)[\s\S]*padding-inline: 1\.5rem/);
  assert.match(css, /padding-block: 5rem/);
  assert.match(css, /@media \(min-width: 48rem\)[\s\S]*padding-block: 8rem/);
});

test('keeps tablet navigation in the compact menu', () => {
  const header = source('../components/napkey/site-header.tsx');
  assert.match(header, /className="hidden items-center gap-1 lg:flex"/);
  assert.match(header, /className="inline-flex[^\n]+lg:hidden"/);
  assert.match(header, /matchMedia\('\(min-width: 64rem\)'\)/);
  assert.match(header, /if \(event\.matches\) setOpen\(false\)/);
});

test('stacks usage chart values without squeezing the mobile bar', () => {
  const chart = source('../components/console/usage-chart.tsx');
  assert.match(chart, /grid-cols-\[4\.25rem_minmax\(0,1fr\)\]/);
  assert.match(chart, /col-span-2[^\n]+sm:col-span-1/);
});

test('makes dense controls horizontally scrollable on narrow screens', () => {
  const integration = source('../components/sections/integration.tsx');
  const shell = source('../components/console/console-shell.tsx');
  assert.match(integration, /overflow-x-auto/);
  assert.match(integration, /flex-nowrap/);
  assert.match(shell, /-mx-4[^\n]+px-4[^\n]+sm:-mx-6/);
});
