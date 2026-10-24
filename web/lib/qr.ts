// QR codes are drawn in the browser from the short URL the API returned
// (D30): no server endpoint, nothing new on the redirect path, and a QR can
// only ever encode one of this service's own links.

export type QrImages = { png: string; svg: string };

// Always black on white, whatever the page theme: scanners expect dark
// modules on a light background.
const options = {
  errorCorrectionLevel: 'M' as const,
  margin: 2,
  color: { dark: '#000000', light: '#ffffff' },
};

// Loaded on demand so the QR library is not part of the first page load.
export async function makeQr(text: string): Promise<QrImages> {
  const QRCode = (await import('qrcode')).default;
  const [png, svg] = await Promise.all([
    QRCode.toDataURL(text, { ...options, width: 1024 }),
    QRCode.toString(text, { ...options, type: 'svg' }),
  ]);
  return { png, svg: 'data:image/svg+xml;charset=utf-8,' + encodeURIComponent(svg) };
}

// File name for a download: the short URL without the scheme, made safe.
export function qrFileName(shortUrl: string, ext: 'png' | 'svg'): string {
  const base = shortUrl.replace(/^https?:\/\//, '').replace(/[^A-Za-z0-9._-]+/g, '-');
  return `${base}.${ext}`;
}
