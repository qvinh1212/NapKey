import assert from 'node:assert/strict';
import { existsSync, readFileSync } from 'node:fs';
import test from 'node:test';

const appRoot = new URL('../', import.meta.url);

test('publishes a localized docs route backed by shared developer helpers', () => {
  const pageUrl = new URL('app/[locale]/docs/page.tsx', appRoot);
  const workbenchUrl = new URL('components/docs/docs-workbench.tsx', appRoot);

  assert.equal(existsSync(pageUrl), true);
  assert.equal(existsSync(workbenchUrl), true);

  const page = readFileSync(pageUrl, 'utf8');
  const workbench = readFileSync(workbenchUrl, 'utf8');
  assert.match(page, /readModelCatalog/);
  assert.match(workbench, /developerSnippet/);
  assert.match(page, /diagnoseApiFailure/);
});

test('links docs from public navigation, integration CTA, footer, and sitemap', () => {
  const header = readFileSync(new URL('components/napkey/site-header.tsx', appRoot), 'utf8');
  const footer = readFileSync(new URL('components/napkey/site-footer.tsx', appRoot), 'utf8');
  const integration = readFileSync(new URL('components/sections/integration.tsx', appRoot), 'utf8');
  const sitemap = readFileSync(new URL('app/sitemap.ts', appRoot), 'utf8');

  assert.match(header, /href: '\/docs'/);
  assert.match(footer, /docs: '\/docs'/);
  assert.match(integration, /href="\/docs"/);
  assert.match(sitemap, /'\/docs'/);
});

for (const locale of ['vi', 'en']) {
  test(`${locale} docs cover quickstart, API, billing, and troubleshooting`, () => {
    const messages = JSON.parse(
      readFileSync(new URL(`../messages/${locale}.json`, appRoot), 'utf8'),
    );
    const docs = messages.docsPage;

    assert.ok(docs.quickstart);
    assert.ok(docs.api);
    assert.ok(docs.billing);
    assert.ok(docs.errors);
    assert.equal(docs.api.endpoints.messages.path, 'POST /v1/messages');
    assert.equal(docs.api.endpoints.models.path, 'GET /v1/models');
  });
}

