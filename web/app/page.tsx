'use client';

import { useState } from 'react';
import { createLink, type CreateResponse } from '@/lib/api';

export default function Page() {
  const [url, setUrl] = useState('');
  const [customAlias, setCustomAlias] = useState('');
  const [result, setResult] = useState<CreateResponse | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);
  const [copied, setCopied] = useState(false);

  async function onSubmit(e: React.FormEvent) {
    e.preventDefault();
    setSubmitting(true);
    setError(null);
    setResult(null);
    setCopied(false);

    try {
      const res = await createLink({ url, customAlias });
      if (res.ok) {
        setResult(res.data);
        setCustomAlias('');
      } else {
        setError(res.message);
      }
    } finally {
      setSubmitting(false);
    }
  }

  async function onCopy() {
    if (!result) return;
    await navigator.clipboard.writeText(result.short_url);
    setCopied(true);
  }

  return (
    <div className="wrap">
      <h1>
        Shortlink. <span className="highlight">Short links on afh.my.id.</span>
      </h1>

      <form onSubmit={onSubmit}>
        <label htmlFor="url">Long URL</label>
        <input
          id="url"
          name="url"
          type="text"
          inputMode="url"
          autoComplete="off"
          required
          autoFocus
          placeholder="example.com/a/very/long/path"
          value={url}
          onChange={(e) => setUrl(e.target.value)}
        />
        <p className="hint">No scheme needed — https:// is assumed.</p>

        <label htmlFor="alias">Custom alias (optional)</label>
        <input
          id="alias"
          name="custom_alias"
          type="text"
          autoComplete="off"
          placeholder="cv"
          pattern="[a-zA-Z0-9_\-]+"
          minLength={3}
          maxLength={32}
          value={customAlias}
          onChange={(e) => setCustomAlias(e.target.value)}
        />
        <p className="hint">3–32 characters: letters, digits, underscore, hyphen.</p>

        <button type="submit" disabled={submitting || !url}>
          {submitting ? 'Shortening…' : 'Shorten'}
          <span aria-hidden="true">→</span>
        </button>
      </form>

      {/* role=alert so screen readers announce results and errors. */}
      {error && (
        <p className="error" role="alert">
          {error}
        </p>
      )}

      {result && (
        <div className="result" role="alert">
          <a href={result.short_url}>{result.short_url}</a>
          <button type="button" className="outline" onClick={onCopy}>
            {copied ? 'Copied' : 'Copy'}
          </button>
        </div>
      )}
    </div>
  );
}
