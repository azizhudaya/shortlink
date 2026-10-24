'use client';

import { useEffect, useState } from 'react';

// D33: the page follows the device's light/dark setting by default. The sun
// or moon shows the theme in use; a click flips it and remembers the choice
// in this browser. Flipping back to what the device prefers forgets the choice,
// so the page follows the device again.
export const themeStorageKey = 'theme';

// Applies a stored choice before first paint (inlined in layout.tsx), so the
// page never flashes the other theme.
export const themeScript = `try{var t=localStorage.getItem('${themeStorageKey}');if(t==='light'||t==='dark')document.documentElement.dataset.theme=t}catch(e){}`;

const darkQuery = '(prefers-color-scheme: dark)';

function isDark(): boolean {
  const chosen = document.documentElement.dataset.theme;
  if (chosen === 'light' || chosen === 'dark') return chosen === 'dark';
  return window.matchMedia(darkQuery).matches;
}

function setTheme(dark: boolean) {
  const root = document.documentElement;
  const followsDevice = dark === window.matchMedia(darkQuery).matches;
  if (followsDevice) delete root.dataset.theme;
  else root.dataset.theme = dark ? 'dark' : 'light';
  try {
    if (followsDevice) localStorage.removeItem(themeStorageKey);
    else localStorage.setItem(themeStorageKey, dark ? 'dark' : 'light');
  } catch {
    // Storage can be blocked (private mode); the choice then lasts this page only.
  }
}

export default function ThemeToggle() {
  // Only the accessible label needs JavaScript; which icon shows is pure CSS,
  // so it is right on first paint.
  const [dark, setDark] = useState<boolean | null>(null);

  useEffect(() => {
    setDark(isDark());
    const mq = window.matchMedia(darkQuery);
    const onChange = () => setDark(isDark());
    mq.addEventListener('change', onChange);
    return () => mq.removeEventListener('change', onChange);
  }, []);

  const label = dark === null ? 'Switch theme' : dark ? 'Switch to light theme' : 'Switch to dark theme';

  return (
    <button
      type="button"
      className="theme-toggle"
      aria-label={label}
      title={label}
      onClick={() => {
        const next = !isDark();
        setTheme(next);
        setDark(next);
      }}
    >
      <svg className="icon-sun" viewBox="0 0 24 24" width="18" height="18" aria-hidden="true" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round">
        <circle cx="12" cy="12" r="4" />
        <path d="M12 2v2M12 20v2M4.93 4.93l1.41 1.41M17.66 17.66l1.41 1.41M2 12h2M20 12h2M4.93 19.07l1.41-1.41M17.66 6.34l1.41-1.41" />
      </svg>
      <svg className="icon-moon" viewBox="0 0 24 24" width="18" height="18" aria-hidden="true" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
        <path d="M21 12.79A9 9 0 1 1 11.21 3a7 7 0 0 0 9.79 9.79z" />
      </svg>
    </button>
  );
}
