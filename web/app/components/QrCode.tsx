'use client';

import { useEffect, useId, useState } from 'react';
import { makeQr, qrFileName, type QrImages } from '@/lib/qr';

// A "Show QR Code" toggle that reveals the code for a short URL with PNG and SVG
// downloads. Nothing is generated until it is first opened.
export default function QrCode({ shortUrl }: { shortUrl: string }) {
  const [open, setOpen] = useState(false);
  const [images, setImages] = useState<QrImages | null>(null);
  const [failed, setFailed] = useState(false);
  const panelId = useId();

  useEffect(() => {
    if (!open || images) return;
    let cancelled = false;
    makeQr(shortUrl)
      .then((qr) => !cancelled && setImages(qr))
      .catch(() => !cancelled && setFailed(true));
    return () => {
      cancelled = true;
    };
  }, [open, images, shortUrl]);

  const label = shortUrl.replace(/^https?:\/\//, '');

  return (
    <div className="qr">
      <button
        type="button"
        className="outline"
        aria-expanded={open}
        aria-controls={panelId}
        onClick={() => setOpen((o) => !o)}
      >
        {open ? 'Hide QR Code' : 'Show QR Code'}
      </button>

      {open && (
        <div id={panelId} className="qr-panel">
          {failed && <p className="error">Couldn&apos;t make the QR code. Try again.</p>}
          {!failed && !images && <p className="hint">Generating…</p>}
          {images && (
            <>
              {/* eslint-disable-next-line @next/next/no-img-element -- a data URL, not an optimizable asset */}
              <img src={images.png} width={192} height={192} alt={`QR code for ${label}`} />
              <div className="qr-downloads">
                <a className="button small" href={images.png} download={qrFileName(shortUrl, 'png')}>
                  Download PNG
                </a>
                <a className="button small" href={images.svg} download={qrFileName(shortUrl, 'svg')}>
                  Download SVG
                </a>
              </div>
            </>
          )}
        </div>
      )}
    </div>
  );
}
