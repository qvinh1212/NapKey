'use client';

import { createContext, useCallback, useContext, useEffect, useMemo, useState } from 'react';
import type { ReactNode } from 'react';
import { useRouter } from '@/i18n/navigation';
import { api } from '@/lib/api/client';
import type { SessionResponse, SessionUser } from '@/lib/api/types';

/**
 * Nguon su that duy nhat cho "ai dang dang nhap" trong console.
 *
 * Phien duoc kiem o phia client chu khong o middleware. Ly do: cookie phien la
 * HttpOnly va chi napkey-core xac thuc duoc no - middleware cua Next khong the biet
 * cookie con hieu luc hay khong ma khong goi mot chang mang tren MOI dieu huong,
 * ke ca dieu huong tinh. Kiem mot lan khi console mount thi doi lai mot nhay man
 * hinh loading, nhung khong bien moi lan doi trang thanh mot round-trip.
 *
 * Quan trong: day chi la lop trai nghiem. Uy quyen that nam o napkey-core, moi
 * endpoint tu kiem phien va tra 401. An mot cai nut o day khong phai la bao mat.
 */

type SessionState =
  | { status: 'loading' }
  | { status: 'authenticated'; user: SessionUser; permissions: string[]; expiresAt: string }
  | { status: 'anonymous' };

type SessionContextValue = SessionState & {
  /** Doc lai phien tu server, dung sau khi dang nhap hoac doi mat khau. */
  refresh: () => Promise<void>;
  signOut: () => Promise<void>;
};

const SessionContext = createContext<SessionContextValue | null>(null);

/**
 * Doc phien, tra ve trang thai chu khong tu set.
 *
 * Tach phan goi mang ra khoi component de ca effect luc mount va ham `refresh`
 * dung chung mot duong doc, va de phan setState nam dung noi goi.
 *
 * Moi loi deu thanh `anonymous`: 401 la cau tra loi binh thuong cho khach chua dang
 * nhap, con loi mang thi console cung khong lam gi duoc khi khong biet nguoi dung la ai.
 */
async function fetchSession(signal?: AbortSignal): Promise<SessionState> {
  try {
    const data = await api.get<SessionResponse>('/v1/auth/session', signal);
    return { status: 'authenticated', user: data.user, permissions: data.permissions, expiresAt: data.expiresAt };
  } catch {
    return { status: 'anonymous' };
  }
}

export function SessionProvider({ children }: { children: ReactNode }) {
  const [state, setState] = useState<SessionState>({ status: 'loading' });
  const router = useRouter();

  useEffect(() => {
    const controller = new AbortController();

    async function run() {
      const next = await fetchSession(controller.signal);
      // Bo qua ket qua neu component da unmount: set state luc do la mot canh bao
      // vo ich trong console cua nguoi phat trien.
      if (!controller.signal.aborted) setState(next);
    }

    void run();
    return () => controller.abort();
  }, []);

  const refresh = useCallback(async () => {
    setState(await fetchSession());
  }, []);

  const signOut = useCallback(async () => {
    try {
      await api.post('/v1/auth/logout');
    } finally {
      // Xoa phien phia client du server co tra loi hay khong: nguoi dung da bam
      // dang xuat, giu ho o trang thai da dang nhap la sai.
      setState({ status: 'anonymous' });
      router.replace('/signin');
    }
  }, [router]);

  const value = useMemo<SessionContextValue>(
    () => ({ ...state, refresh, signOut }),
    [state, refresh, signOut],
  );

  return <SessionContext.Provider value={value}>{children}</SessionContext.Provider>;
}

export function useSession(): SessionContextValue {
  const ctx = useContext(SessionContext);
  if (!ctx) throw new Error('useSession phai duoc dung ben trong SessionProvider');
  return ctx;
}
