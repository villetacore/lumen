"use client";

import dynamic from "next/dynamic";

// The shell reads window, localStorage and the URL from its first render, and
// the prebuilt pages know neither the user nor the real params, so everything
// from here down renders only in the browser.
export default dynamic(() => import("../Shell"), { ssr: false });
