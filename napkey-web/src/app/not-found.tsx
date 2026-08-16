import Link from 'next/link';

export default function NotFound() {
  return (
    <html lang="vi">
      <body
        style={{
          background: '#121317',
          color: '#fff',
          fontFamily: 'ui-sans-serif, system-ui, sans-serif',
          display: 'grid',
          placeItems: 'center',
          minHeight: '100dvh',
          margin: 0,
        }}
      >
        <main style={{ textAlign: 'center', padding: '2rem' }}>
          <p style={{ fontFamily: 'ui-monospace, monospace', color: '#0086ff', fontSize: 13 }}>404</p>
          <h1 style={{ fontSize: 32, margin: '0.5rem 0 1rem', letterSpacing: '-0.02em' }}>
            Không tìm thấy trang
          </h1>
          <Link href="/vi" style={{ color: '#a3a3a3', fontSize: 13 }}>
            Về trang chủ
          </Link>
        </main>
      </body>
    </html>
  );
}
