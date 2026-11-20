import { NextResponse, type NextRequest } from "next/server";

export function proxy(request: NextRequest) {
  if (process.env.CALLS_E2E_PROBE !== "1" && ["/calls/probe", "/calls/spike"].includes(request.nextUrl.pathname)) {
    return new NextResponse(null, { status: 404 });
  }
  return NextResponse.next();
}

export const config = { matcher: ["/calls/probe", "/calls/spike"] };
