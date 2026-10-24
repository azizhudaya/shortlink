'use client';

import Link from 'next/link';
import { useRouter, useSearchParams } from 'next/navigation';
import { Suspense, useEffect } from 'react';
import { useSession } from '@/lib/session';

// The callback's failure codes (D11), in words.
const errorCopy: Record<string, string> = {
  signups_closed: 'New sign-ups are temporarily paused. Please try again later.',
  email_unverified: "Your Google account's email isn't verified.",
  account_disabled: 'This account has been disabled.',
  failed: "Sign-in didn't complete. Please try again.",
};

function SignInError() {
  const code = useSearchParams().get('error');
  if (!code) return null;
  return (
    <p className="error" role="alert">
      {errorCopy[code] ?? errorCopy.failed}
    </p>
  );
}

export default function SignInPage() {
  const { status } = useSession();
  const router = useRouter();

  useEffect(() => {
    if (status === 'authed') router.replace('/links');
  }, [status, router]);

  return (
    <div className="wrap">
      {/* eslint-disable-next-line @next/next/no-img-element -- a small static logo, no optimisation needed */}
      <img className="brand-logo" src="/afh-logo.png" width={59} height={48} alt="AFH" />
      <h1>
        AFH Shortlink. <span className="highlight">Sign in to create short links.</span>
      </h1>

      {/* A plain link, not a fetch: sign-in is a full navigation to Google
          and works without JavaScript. */}
      <a className="button signin-cta" href="/api/auth/google/start">
        Continue with Google
        <span aria-hidden="true">→</span>
      </a>

      <Suspense fallback={null}>
        <SignInError />
      </Suspense>

      <p className="hint">
        We only use your Google account to sign you in, and never see your password.{' '}
        <Link href="/privacy">Privacy</Link>
      </p>
    </div>
  );
}
