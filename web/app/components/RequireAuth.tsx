'use client';

import { useRouter } from 'next/navigation';
import { useEffect } from 'react';
import { useSession } from '@/lib/session';

// RequireAuth renders its children only for a signed-in user and sends
// everyone else to /signin.
export default function RequireAuth({ children }: { children: React.ReactNode }) {
  const { status } = useSession();
  const router = useRouter();

  useEffect(() => {
    if (status === 'anon') router.replace('/signin');
  }, [status, router]);

  if (status !== 'authed') {
    return (
      <div className="page-loading" role="status">
        <span className="spinner" aria-hidden="true" />
        Loading…
      </div>
    );
  }
  return <>{children}</>;
}
