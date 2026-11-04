import { NextResponse, type NextRequest } from "next/server";

// Dev server only (see pageExtensions in next.config.mjs). Adds LUMEN_TOKEN to
// proxied API requests, so the token never reaches the browser.
export function proxy(request: NextRequest) {
  const token = process.env.LUMEN_TOKEN;
  if (!token) return NextResponse.next();
  const headers = new Headers(request.headers);
  headers.set("Authorization", `Bearer ${token}`);
  return NextResponse.next({ request: { headers } });
}

export const config = { matcher: "/api/:path*" };
