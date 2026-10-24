import type { Metadata } from 'next';
import Link from 'next/link';

export const metadata: Metadata = {
  title: 'Privacy · Shortlink',
};

// Contact for data requests: the operator's address.
const CONTACT = 'azizhudaya39@gmail.com';

export default function PrivacyPage() {
  return (
    <article className="wrap prose">
      <h1>
        Privacy. <span className="highlight">What Shortlink keeps, and why.</span>
      </h1>

      <h2>When you sign in</h2>
      <p>
        Sign-in uses Google. Shortlink receives and stores only your Google account ID and your email address. It never
        sees your Google password and asks Google for nothing else: no name, no photo, no contacts, and no access to
        your Google account after sign-in.
      </p>
      <p>
        Each signed-in browser has a session, stored with the IP address and browser user-agent it signed in from. A
        session ends after 30 days or when you sign out, and its record is then deleted.
      </p>

      <h2>When you create a link</h2>
      <p>
        Shortlink stores the destination URL, the short code, and when you created it, linked to your account. To keep
        the service free of phishing and malware, every destination is checked against Google Safe Browsing when you
        create it and again periodically for 30 days; the destination URL is sent to Google for that check. A count of
        the links you created in the last 24 hours is kept to enforce the daily limit.
      </p>

      <h2>When someone opens a short link</h2>
      <p>
        Nothing about the visitor is recorded: no IP address, no cookie, no tracking. The redirect also tells the
        destination site nothing about the short link it came from.
      </p>

      <h2>Legal basis and your rights</h2>
      <p>
        Data is processed under Indonesia&apos;s Personal Data Protection Law (UU No. 27 Tahun 2022, &ldquo;UU
        PDP&rdquo;) to provide the service you signed up for and to prevent abuse of it. You can ask to see, correct, or
        delete your data, which deletes your account and its links, by writing to{' '}
        <a href={`mailto:${CONTACT}`}>{CONTACT}</a> from the email address you sign in with.
      </p>

      <p className="hint">
        <Link href="/signin">Back to sign-in</Link>
      </p>
    </article>
  );
}
