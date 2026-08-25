import type { NextConfig } from "next";

const nextConfig: NextConfig = {
  // Emit a self-contained server (.next/standalone) so the Docker run stage
  // ships only the traced runtime deps instead of the full node_modules.
  output: "standalone",
  async redirects() {
    return [
      // Job detail used to be its own route; it is now the dashboard drawer.
      // Deliberately temporary (307): a permanent redirect is cached by the
      // browser indefinitely, so restoring /jobs/:jobId as a real route later
      // would be invisible to everyone who had already followed the old link.
      {
        source: "/jobs/:jobId",
        destination: "/?job=:jobId",
        permanent: false,
      },
    ];
  },
  async rewrites() {
    const target =
      process.env.KIWI_BACKEND_URL ||
      process.env.NEXT_PUBLIC_KIWI_API_URL ||
      "http://127.0.0.1:8080";
    const proxyToBackend = [
      {
        source: "/api/:path*",
        destination: `${target}/api/:path*`,
      },
      {
        source: "/admin/:path*",
        destination: `${target}/admin/:path*`,
      },
      {
        source: "/auth/:path*",
        destination: `${target}/auth/:path*`,
      },
    ];
    // fallback (not the default afterFiles list a bare array would give) so
    // Next's own dynamic page routes win first — e.g. app/(dashboard)/admin/
    // orgs/[orgId]/page.tsx over the /admin/:path* proxy rule above. With the
    // default ordering, dynamic routes are matched AFTER these rewrites, so
    // that page (and its RSC navigation fetches) got silently proxied to the
    // Go backend instead, which 401'd with "missing Authorization header" —
    // a plain page navigation, not an api.ts call, carries no Bearer token.
    return { fallback: proxyToBackend };
  },
};

export default nextConfig;
