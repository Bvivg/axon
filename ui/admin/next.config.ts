import type { NextConfig } from "next";

const nextConfig: NextConfig = {
  basePath: "/admin",
  output: "standalone",
  reactStrictMode: true,
  allowedDevOrigins: process.env.ALLOWED_DEV_ORIGINS?.split(",").map((host) => host.trim()).filter(Boolean),
  async headers() {
    return [{
      source: "/:path*",
      headers: [
        { key: "X-Content-Type-Options", value: "nosniff" },
        { key: "X-Frame-Options", value: "DENY" },
        { key: "Referrer-Policy", value: "same-origin" },
        { key: "Permissions-Policy", value: "camera=(), microphone=(), display-capture=(), geolocation=()" },
        { key: "Cache-Control", value: "private, no-store" },
      ],
    }];
  },
};

export default nextConfig;
