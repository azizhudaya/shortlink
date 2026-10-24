'use client';

import Link from 'next/link';
import { usePathname, useRouter } from 'next/navigation';
import { useSession } from '@/lib/session';

export default function Nav() {
  const { status, signOut } = useSession();
  const pathname = usePathname();
  const router = useRouter();
  if (status !== 'authed') return null;

  async function onSignOut() {
    await signOut();
    router.replace('/signin');
  }

  return (
    <nav className="nav" aria-label="Main">
      <Link href="/" aria-current={pathname === '/' ? 'page' : undefined}>
        Create
      </Link>
      <Link href="/links" aria-current={pathname === '/links' ? 'page' : undefined}>
        My links
      </Link>
      <button type="button" className="link-button" onClick={onSignOut}>
        Sign out
      </button>
    </nav>
  );
}
