'use client';

import { createContext, useCallback, useContext, useEffect, useState } from 'react';
import { logout, me, setCsrfToken, type Me } from './api';

type Status = 'loading' | 'authed' | 'anon';

type SessionValue = {
  status: Status;
  me: Me | null;
  refresh: () => Promise<void>;
  signOut: () => Promise<void>;
};

const SessionContext = createContext<SessionValue | null>(null);

// SessionProvider asks the API who is signed in. The API is the only
// enforcement point; this only decides what to render (D19).
export function SessionProvider({ children }: { children: React.ReactNode }) {
  const [status, setStatus] = useState<Status>('loading');
  const [user, setUser] = useState<Me | null>(null);

  const refresh = useCallback(async () => {
    const res = await me();
    if (res.ok) {
      setCsrfToken(res.data.csrf_token);
      setUser(res.data);
      setStatus('authed');
    } else {
      setCsrfToken(null);
      setUser(null);
      setStatus('anon');
    }
  }, []);

  const signOut = useCallback(async () => {
    await logout();
    setCsrfToken(null);
    setUser(null);
    setStatus('anon');
  }, []);

  useEffect(() => {
    void refresh();
  }, [refresh]);

  return (
    <SessionContext.Provider value={{ status, me: user, refresh, signOut }}>{children}</SessionContext.Provider>
  );
}

export function useSession(): SessionValue {
  const v = useContext(SessionContext);
  if (!v) throw new Error('useSession must be used inside SessionProvider');
  return v;
}
