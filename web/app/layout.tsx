import type { Metadata } from 'next';
import { Geist } from 'next/font/google';
import './globals.css';

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
    <html lang="en" className={geistSans.variable}>
      <body>
        <main>{children}</main>
      </body>
    </html>
  );
}
