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
      <h1>
        Shortlink. <span className="highlight">Sign in to create short links.</span>
      </h1>

      {/* A plain link, not a fetch: sign-in is a full navigation to Google
          and works without JavaScript. */}
      <a className="button" href="/api/auth/google/start">
        Continue with Google
        <span aria-hidden="true">→</span>
      </a>

      <Suspense fallback={null}>
        <SignInError />
      </Suspense>

      <p className="hint">
        Shortlink stores only your Google email and account ID. We never see your Google password.{' '}
        <Link href="/privacy">Privacy</Link>
      </p>
    </div>
  );
}
