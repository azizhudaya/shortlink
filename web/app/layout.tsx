import type { Metadata } from 'next';
import { Geist } from 'next/font/google';
import './globals.css';
import Nav from './components/Nav';
import ThemeToggle, { themeScript } from './components/ThemeToggle';
import { SessionProvider } from '@/lib/session';

// Same typeface as the personal site. next/font downloads it at build time
// and serves it from this app, so the browser never calls Google Fonts.
const geistSans = Geist({
  variable: '--font-geist-sans',
  subsets: ['latin'],
  display: 'swap',
});

export const metadata: Metadata = {
  title: 'Shortlink',
  description: 'Create short links on afh.my.id',
  // Private tool, not a public page: keep it out of search engines.
  robots: { index: false, follow: false },
};

export default function RootLayout({ children }: { children: React.ReactNode }) {
  return (
    // suppressHydrationWarning: themeScript may set data-theme on <html>
    // before React hydrates, which is intended (D33).
    <html lang="en" className={geistSans.variable} suppressHydrationWarning>
      <head>
        {/* Runs before first paint so a stored theme never flashes the other one. */}
        <script dangerouslySetInnerHTML={{ __html: themeScript }} />
      </head>
      <body>
        <SessionProvider>
          <header className="topbar">
            <Nav />
            <ThemeToggle />
          </header>
          <main>{children}</main>
        </SessionProvider>
      </body>
    </html>
  );
}
