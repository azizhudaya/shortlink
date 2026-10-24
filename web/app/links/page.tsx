'use client';

import { useRouter } from 'next/navigation';
import { useCallback, useEffect, useState } from 'react';
import { deleteLink, listLinks, setDisabled, type ApiResult, type LinkItem } from '@/lib/api';
import { useSession } from '@/lib/session';
import QrCode from '../components/QrCode';
import RequireAuth from '../components/RequireAuth';

export default function LinksPage() {
  return (
    <RequireAuth>
      <MyLinks />
    </RequireAuth>
  );
}

const statusLabel: Record<LinkItem['status'], string> = {
  active: 'Active',
  disabled: 'Disabled',
  taken_down: 'Disabled by operator',
};

function MyLinks() {
  const { signOut } = useSession();
  const router = useRouter();
  const [links, setLinks] = useState<LinkItem[]>([]);
  const [cursor, setCursor] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState<string | null>(null);
  const [announcement, setAnnouncement] = useState('');
  const [error, setError] = useState<string | null>(null);

  // Any 401 means the session ended under us: start over at sign-in.
  const handle = useCallback(
    async <T,>(res: ApiResult<T>): Promise<ApiResult<T>> => {
      if (!res.ok && res.status === 401) {
        await signOut();
        router.replace('/signin');
      }
      return res;
    },
    [signOut, router],
  );

  const load = useCallback(
    async (after?: string) => {
      setLoading(true);
      const res = await handle(await listLinks(after));
      setLoading(false);
      if (!res.ok) {
        setError(res.message);
        return;
      }
      setError(null);
      setLinks((prev) => (after ? [...prev, ...res.data.links] : res.data.links));
      setCursor(res.data.next_cursor);
    },
    [handle],
  );

  useEffect(() => {
    void load();
  }, [load]);

  async function onToggle(link: LinkItem) {
    const disable = link.status === 'active';
    setBusy(link.code);
    const res = await handle(await setDisabled(link.code, disable));
    setBusy(null);
    if (!res.ok) {
      setAnnouncement(res.message);
      return;
    }
    setLinks((prev) => prev.map((l) => (l.code === link.code ? { ...l, status: disable ? 'disabled' : 'active' } : l)));
    setAnnouncement(`${link.short_url} ${disable ? 'disabled' : 'enabled'}.`);
  }

  async function onDelete(link: LinkItem) {
    if (!window.confirm(`Delete ${link.short_url}? It will stop working, and this code can't be used again.`)) return;
    setBusy(link.code);
    const res = await handle(await deleteLink(link.code));
    setBusy(null);
    if (!res.ok) {
      setAnnouncement(res.message);
      return;
    }
    setLinks((prev) => prev.filter((l) => l.code !== link.code));
    setAnnouncement(`${link.short_url} deleted.`);
  }

  return (
    <div className="wrap">
      <h1>
        My links. <span className="highlight">Newest first.</span>
      </h1>

      <p className="visually-hidden" aria-live="polite">
        {announcement}
      </p>
      {error && (
        <p className="error" role="alert">
          {error}
        </p>
      )}
      {!loading && !error && links.length === 0 && <p className="hint">No links yet.</p>}

      <ul className="links">
        {links.map((link) => (
          <LinkRow
            key={link.code}
            link={link}
            busy={busy === link.code}
            onToggle={() => onToggle(link)}
            onDelete={() => onDelete(link)}
          />
        ))}
      </ul>

      {cursor && (
        <button type="button" className="outline" disabled={loading} onClick={() => load(cursor)}>
          {loading ? 'Loading…' : 'Load more'}
        </button>
      )}
    </div>
  );
}

function LinkRow({
  link,
  busy,
  onToggle,
  onDelete,
}: {
  link: LinkItem;
  busy: boolean;
  onToggle: () => void;
  onDelete: () => void;
}) {
  const [copied, setCopied] = useState(false);
  const shortLabel = link.short_url.replace(/^https:\/\//, '');

  async function onCopy() {
    await navigator.clipboard.writeText(link.short_url);
    setCopied(true);
  }

  return (
    <li className="link-row">
      <div className="link-head">
        <a href={link.short_url} className="short">
          {shortLabel}
        </a>
        <span className={`badge badge-${link.status}`}>{statusLabel[link.status]}</span>
      </div>
      <p className="dest" title={link.long_url}>
        {destination(link.long_url)}
      </p>
      <p className="meta">
        Created{' '}
        <time dateTime={link.created_at}>
          {new Date(link.created_at).toLocaleDateString(undefined, { dateStyle: 'medium' })}
        </time>
      </p>
      {/* One row: Copy and QR on the left, icon tools on the right. The QR
          panel, when open, wraps onto its own line below (CSS order). */}
      <div className="actions">
        <button type="button" className="outline" onClick={onCopy}>
          {copied ? 'Copied' : 'Copy'}
        </button>
        {/* Nothing else for a taken-down link: a QR or a toggle would only lead to a 410. */}
        {link.status !== 'taken_down' && (
          <>
            <QrCode shortUrl={link.short_url} />
            <div className="row-tools">
              <IconButton
                label={`${link.status === 'active' ? 'Disable' : 'Enable'} ${shortLabel}`}
                disabled={busy}
                onClick={onToggle}
              >
                {link.status === 'active' ? <PauseIcon /> : <PlayIcon />}
              </IconButton>
              <IconButton label={`Delete ${shortLabel}`} danger disabled={busy} onClick={onDelete}>
                <TrashIcon />
              </IconButton>
            </div>
          </>
        )}
      </div>
    </li>
  );
}

function IconButton({
  label,
  danger,
  disabled,
  onClick,
  children,
}: {
  label: string;
  danger?: boolean;
  disabled: boolean;
  onClick: () => void;
  children: React.ReactNode;
}) {
  return (
    <button
      type="button"
      className={danger ? 'icon-button danger' : 'icon-button'}
      aria-label={label}
      title={label}
      disabled={disabled}
      onClick={onClick}
    >
      {children}
    </button>
  );
}

const iconProps = {
  viewBox: '0 0 24 24',
  width: 18,
  height: 18,
  fill: 'none',
  stroke: 'currentColor',
  strokeWidth: 2,
  strokeLinecap: 'round' as const,
  strokeLinejoin: 'round' as const,
  'aria-hidden': true,
};

function PauseIcon() {
  return (
    <svg {...iconProps}>
      <path d="M9 5v14M15 5v14" />
    </svg>
  );
}

function PlayIcon() {
  return (
    <svg {...iconProps}>
      <path d="M7 5l12 7-12 7z" />
    </svg>
  );
}

function TrashIcon() {
  return (
    <svg {...iconProps}>
      <path d="M4 7h16M10 11v6M14 11v6M6 7l1 13h10l1-13M9 7V4h6v3" />
    </svg>
  );
}

// Host plus a truncated path; the full URL is in the title attribute.
function destination(raw: string): string {
  try {
    const u = new URL(raw);
    const rest = u.pathname + u.search;
    const path = rest === '/' ? '' : rest.length > 40 ? rest.slice(0, 39) + '…' : rest;
    return u.host + path;
  } catch {
    return raw;
  }
}
