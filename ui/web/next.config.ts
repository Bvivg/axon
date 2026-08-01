import type { NextConfig } from "next";

const nextConfig: NextConfig = {
  // Traces the modules the app actually reaches and emits a self-contained
  // server, so the runtime image carries neither node_modules nor the source.
  output: "standalone",

  // Every call goes to the gateway over Connect, so nothing here proxies or
  // rewrites: the browser talks to the trust boundary directly, and the origin
  // it is allowed to talk from is the gateway's CORS allow-list.
  reactStrictMode: true,
};

export default nextConfig;
