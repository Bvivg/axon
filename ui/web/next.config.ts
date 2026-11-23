import type { NextConfig } from "next";

const nextConfig: NextConfig = {

  output: "standalone",

  reactStrictMode: true,

  allowedDevOrigins: process.env.ALLOWED_DEV_ORIGINS?.split(",").map((host) => host.trim()).filter(Boolean),

  async headers() {
    return [{
      source: "/:path*",
      headers: [
        { key: "X-Content-Type-Options", value: "nosniff" },
        { key: "X-Frame-Options", value: "DENY" },
        { key: "Referrer-Policy", value: "strict-origin-when-cross-origin" },
        { key: "Content-Security-Policy", value: "frame-ancestors 'none'; object-src 'none'; base-uri 'self'" },
        { key: "Permissions-Policy", value: "camera=(self), microphone=(self), display-capture=(self), geolocation=()" },
      ],
    }, {
      source: "/embed/:path*",
      headers: [
        { key: "X-Frame-Options", value: "SAMEORIGIN" },
        { key: "Content-Security-Policy", value: "frame-ancestors 'self'; object-src 'none'; base-uri 'self'" },
      ],
    }];
  },
};

export default nextConfig;
