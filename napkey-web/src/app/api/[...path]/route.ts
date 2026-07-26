import type { NextRequest } from 'next/server';
import { proxyToCore } from '@/lib/api/proxy';

/**
 * Duong ong duy nhat tu console sang napkey-core.
 *
 * Chay tren Node runtime chu khong Edge: no goi mot dia chi trong mang noi bo
 * (`http://napkey-core:8081`), thu ma Edge runtime khong voi tay duoc.
 *
 * `force-dynamic` de chac chan khong co tang cache nao dung giua - so du va usage
 * cua nguoi nay khong bao gio duoc phuc vu cho nguoi khac.
 */
export const runtime = 'nodejs';
export const dynamic = 'force-dynamic';

type Context = { params: Promise<{ path: string[] }> };

async function handle(req: NextRequest, context: Context) {
  const { path } = await context.params;
  return proxyToCore(req, path);
}

export const GET = handle;
export const POST = handle;
export const PATCH = handle;
export const PUT = handle;
export const DELETE = handle;
