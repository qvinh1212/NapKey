import { NextResponse, type NextRequest } from 'next/server';

/**
 * BFF chuyen tiep giua console va napkey-core.
 *
 * Trinh duyet goi `/api/...` cua Next, ham nay chuyen tiep vao napkey-core trong
 * mang noi bo. Nho vay control plane khong can phoi ra internet.
 *
 * Ba dieu can dung o day, vi day la lop chan giua nguoi dung va control plane:
 *
 *  1. Duong dan phai nam trong danh sach cho phep. Khong chuyen tiep tuy y, neu
 *     khong `/api/v1/admin/...` hoac `/api/internal/usage` se lo ra ngoai qua
 *     chinh console - tuc bien BFF thanh mot lo hong thay vi mot lop chan.
 *  2. Chi chuyen tiep dung nhung header can thiet. Chuyen tiep tat ca se mang theo
 *     `X-Internal-Token` do khach tu dat, va napkey-core tin header do.
 *  3. Cookie di ca hai chieu, giu nguyen `Set-Cookie` de HttpOnly va Secure khong
 *     bi mat tren duong ve.
 */

/** Dia chi noi bo cua napkey-core. Khong co tien to NEXT_PUBLIC_ - khong ra client. */
const CORE_URL = (process.env.NAPKEY_CORE_URL ?? 'http://127.0.0.1:8081').replace(/\/+$/, '');

/**
 * Duong dan console duoc phep goi.
 *
 * Danh sach cho phep, khong phai danh sach chan: mot endpoint moi ben backend phai
 * duoc them vao day mot cach co y thuc. Khong co `/v1/admin/*` va khong co
 * `/internal/*` - hai nhom do khong bao gio duoc di qua trinh duyet.
 */
const ALLOWED: readonly (RegExp | string)[] = [
  '/v1/auth/register',
  '/v1/auth/login',
  '/v1/auth/logout',
  '/v1/auth/verify-email',
  '/v1/auth/resend-verification',
  '/v1/auth/forgot-password',
  '/v1/auth/reset-password',
  '/v1/auth/session',
  '/v1/me/password',
  '/v1/me/usage',
  '/v1/me/usage/detail',
  '/v1/me/usage/records',
  '/v1/me/wallet',
  '/v1/me/topups',
  /^\/v1\/me\/topups\/[A-Za-z0-9-]{1,64}$/,
  '/v1/keys',
  /^\/v1\/keys\/[A-Za-z0-9-]{1,64}$/,
	'/v1/admin/operations/summary',
	'/v1/admin/operations/alerts',
	'/v1/admin/operations/reconcile-wallets',
	/^\/v1\/admin\/users\/[A-Za-z0-9-]{1,64}\/roles$/,
];

function isAllowed(pathname: string): boolean {
  return ALLOWED.some((rule) =>
    typeof rule === 'string' ? rule === pathname : rule.test(pathname),
  );
}

/**
 * Header duoc chuyen tiep len napkey-core.
 *
 * Danh sach ngan co chu y. Dac biet KHONG co `X-Internal-Token` (bi mat giua hai
 * service, khong phai cua nguoi dung) va khong co `X-Forwarded-For` do client tu
 * dat - de Next/proxy that dat, neu khong ai cung gia duoc khoa rate limit.
 */
const FORWARD_REQUEST_HEADERS = ['content-type', 'cookie', 'x-csrf-token', 'accept-language'];

/** Header duoc tra ve trinh duyet. */
const FORWARD_RESPONSE_HEADERS = ['content-type', 'cache-control'];

export async function proxyToCore(req: NextRequest, path: string[]): Promise<NextResponse> {
  const pathname = '/' + path.join('/');

  if (!isAllowed(pathname)) {
    // Tra 404 chu khong 403: xac nhan mot duong dan co ton tai la chi cho ke tan
    // cong biet cho de nham vao.
    return NextResponse.json({ error: { code: 'not_found', message: 'not found' } }, { status: 404 });
  }

  const target = new URL(CORE_URL + pathname);
  target.search = req.nextUrl.search;

  const headers = new Headers();
  for (const name of FORWARD_REQUEST_HEADERS) {
    const value = req.headers.get(name);
    if (value) headers.set(name, value);
  }

  // Origin phai la origin cua console, vi napkey-core kiem CORS theo PUBLIC_BASE_URL.
  // Lay tu request thay vi hardcode, de dev tren localhost khong phai sua config.
  headers.set('Origin', req.nextUrl.origin);

  let body: string | undefined;
  if (req.method !== 'GET' && req.method !== 'HEAD') {
    body = await req.text();
  }

  let upstream: Response;
  try {
    upstream = await fetch(target, {
      method: req.method,
      headers,
      body,
      redirect: 'manual',
      // Khong cache: day la du lieu theo phien.
      cache: 'no-store',
    });
  } catch {
    // Control plane khong voi tay duoc. 503 de UI noi dung chuyen dang xay ra thay
    // vi hien mot loi chung khong ai hieu.
    return NextResponse.json(
      {
        error: {
          code: 'internal_error',
          message: 'Khong ket noi duoc control plane. Thu lai sau it phut.',
        },
      },
      { status: 503 },
    );
  }

  const responseBody = await upstream.text();
  const res = new NextResponse(responseBody || null, { status: upstream.status });

  for (const name of FORWARD_RESPONSE_HEADERS) {
    const value = upstream.headers.get(name);
    if (value) res.headers.set(name, value);
  }

  // Set-Cookie phai giu nguyen tung dong: co the co nhieu cookie (phien + CSRF) va
  // gop lai bang dau phay se lam trinh duyet doc sai.
  const cookies = upstream.headers.getSetCookie();
  for (const cookie of cookies) {
    res.headers.append('Set-Cookie', cookie);
  }

  return res;
}
