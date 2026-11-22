import { NextResponse, type NextRequest } from "next/server";

import { contentSecurityPolicy, newNonce } from "@/lib/security/csp";

const probePaths = ["/calls/probe", "/calls/spike"];

const adminPrefix = "/admin";

function adminTarget(request: NextRequest): URL | null {
  const { pathname, search } = request.nextUrl;
  if (pathname !== adminPrefix && !pathname.startsWith(`${adminPrefix}/`)) return null;
  const upstream = process.env.ADMIN_WEB_URL;
  if (!upstream) return null;
  return new URL(`${pathname}${search}`, upstream);
}

export function proxy(request: NextRequest) {
  const admin = adminTarget(request);
  if (admin) return NextResponse.rewrite(admin);

  if (process.env.CALLS_E2E_PROBE !== "1" && probePaths.includes(request.nextUrl.pathname)) {
    return new NextResponse(null, { status: 404 });
  }

  const nonce = newNonce();
  const policy = contentSecurityPolicy(nonce, {
    gateway: process.env.NEXT_PUBLIC_API_BASE_URL ?? "http://localhost:18080",
    assets: process.env.AVATARS_PUBLIC_URL,
    development: process.env.NODE_ENV === "development",
  });

  const headers = new Headers(request.headers);
  headers.set("x-nonce", nonce);
  headers.set("Content-Security-Policy", policy);
  const response = NextResponse.next({ request: { headers } });
  response.headers.set("Content-Security-Policy", policy);
  return response;
}

export const config = {
  matcher: [
    {
      source: "/((?!_next/static|_next/image|favicon.ico|icon.svg|apple-icon.png|sw.js|calls-sw.js).*)",
      missing: [
        { type: "header", key: "next-router-prefetch" },
        { type: "header", key: "purpose", value: "prefetch" },
      ],
    },
    "/calls/probe",
    "/calls/spike",
    "/admin",
    "/admin/:path*",
  ],
};
