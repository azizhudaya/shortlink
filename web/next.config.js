/** @type {import('next').NextConfig} */
const nextConfig = {
  // Minimal self-contained bundle, keeps the image under the 192 MB limit.
  output: 'standalone',

  // Suppress the framework's own identifying header.
  poweredByHeader: false,

  // In production, Caddy proxies app.afh.my.id/api/* to the API container, so
  // no rewrite is involved. This exists only so `npm run dev` can reach a
  // locally-running API at the same same-origin /api/* paths the deployed app
  // uses — without it, local development would need a different fetch URL
  // than production, which is exactly the kind of divergence that hides bugs.
  async rewrites() {
    if (process.env.NODE_ENV === 'production') return [];
    return [
      {
        source: '/api/:path*',
        destination: `${process.env.DEV_API_ORIGIN ?? 'http://localhost:8080'}/api/:path*`,
      },
    ];
  },
};

module.exports = nextConfig;
