"use client";

import dynamic from "next/dynamic";

// The app reads window, localStorage and the URL from the first render, and
// nothing about a user's data is known at build time, so it renders only in
// the browser.
const ClientApp = dynamic(() => import("../../ClientApp"), { ssr: false });

export default function ClientRoot() {
  return <ClientApp />;
}
