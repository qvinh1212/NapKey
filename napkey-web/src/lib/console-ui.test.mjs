import assert from 'node:assert/strict';
import { readdirSync, readFileSync } from 'node:fs';
import test from 'node:test';

const componentsDir = new URL('../components/', import.meta.url);

function read(relative) {
  return readFileSync(new URL(relative, componentsDir), 'utf8');
}

function allComponents() {
  const files = [];
  const walk = (dir, prefix) => {
    for (const entry of readdirSync(new URL(dir, componentsDir), { withFileTypes: true })) {
      if (entry.isDirectory()) walk(`${dir}${entry.name}/`, `${prefix}${entry.name}/`);
      else if (entry.name.endsWith('.tsx')) files.push(`${prefix}${entry.name}`);
    }
  };
  walk('', '');
  return files;
}

/**
 * Icon phai la SVG, khong phai ky tu.
 *
 * Mot ky tu nhu U+2713 hay U+2192 duoc font he thong ve, nen no doi hinh theo may va
 * khong theo duoc do day net cua chu xung quanh. Test nay chan viec dat lai chung vao
 * JSX. Middot U+00B7 duoc phep vi no la dau phan cach trong mot cau, khong phai icon.
 */
test('interface icons are SVG, not text glyphs', () => {
  const banned = ['\u2713', '\u2714', '\u2192', '\u2190', '\u2197', '\u2717', '\u00d7'];
  const offenders = [];
  for (const file of allComponents()) {
    const source = read(file);
    source.split('\n').forEach((line, index) => {
      for (const glyph of banned) {
        if (line.includes(glyph)) offenders.push(`${file}:${index + 1} contains U+${glyph.codePointAt(0).toString(16).toUpperCase().padStart(4, '0')}`);
      }
    });
  }
  assert.deepEqual(offenders, []);
});

/**
 * So o tren mot hang phai bang so tieu de cot.
 *
 * Bang "theo model" tung co 7 tieu de va 7 o, nhung hai o cuoi in cung mot gia tri -
 * mot cot dan nhan "Credit" va mot cot dan nhan "Chi phi", ca hai deu la `row.cost`.
 * Dem so o khong bat duoc loi do, nen test nay dem so BIEU THUC KHAC NHAU: mot bang
 * ma hai cot luon bang nhau thi mot trong hai cot la thua.
 */
test('the by-model table has no column that repeats another', () => {
  const source = read('console/overview.tsx');
  const table = source.slice(source.indexOf('byModelTitle'), source.indexOf('billingMode.'));

  const headers = table.match(/<Th[^>]*>\{tu\('([^']+)'\)\}<\/Th>/g) ?? [];
  const cells = table.match(/<Td[\s\S]*?<\/Td>/g) ?? [];
  assert.equal(headers.length, cells.length, 'header count must match cell count');

  const values = cells.map((cell) => {
    const match = cell.match(/\{(count|compact|money)\(([^)]*)\)/);
    return match ? `${match[1]}(${match[2]})` : cell.replace(/\s+/g, ' ');
  });
  assert.deepEqual(
    values.filter((value, index) => values.indexOf(value) !== index),
    [],
    'two columns render the same expression, so one of them is redundant',
  );
});

/** Trang tong quan phai co khung xuong dung theo bo cuc, khong phai mot dong chu. */
test('the overview shows a skeleton in its own shape while loading', () => {
  const source = read('console/overview.tsx');
  const loading = source.slice(source.indexOf("status === 'loading'"), source.indexOf("status === 'error'"));
  assert.match(loading, /<LoadingStatus/);
  assert.match(loading, /<SkeletonCards/);
  assert.match(loading, /<SkeletonPanel/);
});
