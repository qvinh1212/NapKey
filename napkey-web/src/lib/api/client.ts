import type { ApiErrorBody, ApiErrorCode } from './types';

/**
 * Lop goi API tu trinh duyet.
 *
 * Console KHONG goi napkey-core truc tiep. Moi request di qua `/api/*` cua Next,
 * roi Route Handler chuyen tiep vao mang noi bo (xem `src/lib/api/proxy.ts`).
 * Ly do:
 *
 *  - Cookie phien la HttpOnly va napkey-core chi cho dung origin console. Goi truc
 *    tiep tu browser buoc phai phoi napkey-core ra internet, tuc mo them mot be
 *    mat tan cong chi de tiet mot chang mang.
 *  - Trinh duyet khong bao gio thay dia chi that cua control plane.
 *
 * Danh doi: moi request qua hai chang. Voi mot console doc so lieu thi do tre nay
 * khong dang ke so voi viec phoi control plane ra ngoai.
 */

/** Loi da phan loai, de UI xu ly theo `code` chu khong doc chuoi. */
export class ApiError extends Error {
  readonly status: number;
  readonly code: ApiErrorCode;
  readonly fields?: Record<string, string>;

  constructor(status: number, code: ApiErrorCode, message: string, fields?: Record<string, string>) {
    super(message);
    this.name = 'ApiError';
    this.status = status;
    this.code = code;
    this.fields = fields;
  }

  /** Phien het han hoac chua dang nhap - UI phai dua ve trang dang nhap. */
  get isUnauthenticated(): boolean {
    return this.status === 401;
  }

  /** Da dang nhap nhung chua xac minh email - khong tao duoc key. */
  get needsEmailVerification(): boolean {
    return this.code === 'email_unverified';
  }
}

/** Ten cookie CSRF do napkey-core dat. Doc duoc bang script la co y. */
const CSRF_COOKIE = 'napkey_csrf';
const CSRF_HEADER = 'X-CSRF-Token';

/**
 * Doc cookie CSRF de gan vao header.
 *
 * Day la double-submit: cookie va header phai khop. Cookie khong phai bi mat, no la
 * bang chung same-origin - mot trang khac doc duoc cookie nay thi da co van de lon
 * hon CSRF.
 */
function csrfToken(): string | null {
  if (typeof document === 'undefined') return null;
  const match = document.cookie.match(new RegExp(`(?:^|; )${CSRF_COOKIE}=([^;]*)`));
  const raw = match?.[1];
  return raw === undefined ? null : decodeURIComponent(raw);
}

type RequestOptions = {
  method?: 'GET' | 'POST' | 'PATCH' | 'DELETE';
  body?: unknown;
  signal?: AbortSignal;
};

async function request<T>(path: string, options: RequestOptions = {}): Promise<T> {
  const { method = 'GET', body, signal } = options;
  const headers: Record<string, string> = {};

  if (body !== undefined) headers['Content-Type'] = 'application/json';

  // Chi method thay doi trang thai moi can CSRF. Gan cho GET la vo nghia va lam
  // request that bai khi cookie chua kip duoc dat.
  if (method !== 'GET') {
    const token = csrfToken();
    if (token) headers[CSRF_HEADER] = token;
  }

  const res = await fetch(`/api${path}`, {
    method,
    headers,
    // Bat buoc: khong co dong nay thi cookie phien khong duoc gui kem.
    credentials: 'same-origin',
    body: body === undefined ? undefined : JSON.stringify(body),
    // So lieu usage doi theo tung request, cache lai la hien so cu.
    cache: 'no-store',
    signal,
  });

  if (res.status === 204) return undefined as T;

  const text = await res.text();
  let parsed: unknown = null;
  if (text) {
    try {
      parsed = JSON.parse(text);
    } catch {
      // Body khong phai JSON. Xay ra khi mot lop trung gian (proxy, WAF) tra HTML.
      // Nem loi co ich thay vi de JSON.parse nem loi khong noi len dieu gi.
      throw new ApiError(res.status, 'internal_error', `Phan hoi khong phai JSON (HTTP ${res.status})`);
    }
  }

  if (!res.ok) {
    const errorBody = parsed as ApiErrorBody | null;
    const detail = errorBody?.error;
    throw new ApiError(
      res.status,
      detail?.code ?? 'internal_error',
      detail?.message ?? `Yeu cau that bai (HTTP ${res.status})`,
      detail?.fields,
    );
  }

  return parsed as T;
}

export const api = {
  get: <T>(path: string, signal?: AbortSignal) => request<T>(path, { method: 'GET', signal }),
  post: <T>(path: string, body?: unknown) => request<T>(path, { method: 'POST', body }),
  patch: <T>(path: string, body?: unknown) => request<T>(path, { method: 'PATCH', body }),
  delete: <T>(path: string) => request<T>(path, { method: 'DELETE' }),
};

/**
 * Dung khoang thoi gian thanh query string.
 *
 * Ngay o dang YYYY-MM-DD, backend hieu la nua dem theo gio Viet Nam. Gui RFC3339 tu
 * client se lay moc UTC va lech mat bay tieng - tuc traffic buoi sang cua khach roi
 * sang ngay hom truoc.
 */
export function rangeQuery(params: {
  from?: string;
  to?: string;
  keyId?: string;
  limit?: number;
  offset?: number;
}): string {
  const query = new URLSearchParams();
  if (params.from) query.set('from', params.from);
  if (params.to) query.set('to', params.to);
  if (params.keyId) query.set('keyId', params.keyId);
  if (params.limit !== undefined) query.set('limit', String(params.limit));
  if (params.offset !== undefined) query.set('offset', String(params.offset));
  const qs = query.toString();
  return qs ? `?${qs}` : '';
}