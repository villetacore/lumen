import type { Metadata, Viewport } from "next";
import type { ReactNode } from "react";
import "@villetacore/velessoft-tokens/tokens.css";
import "@villetacore/velessoft-ui/styles.css";
import "../styles.css";
import ClientShell from "./ClientShell";

export const metadata: Metadata = {
  title: "Lumen",
  icons: {
    icon: [
      { url: "/favicon.svg", type: "image/svg+xml" },
      { url: "/favicon-32.png", type: "image/png", sizes: "32x32" },
    ],
    apple: "/apple-touch-icon.png",
  },
};

export const viewport: Viewport = { themeColor: "#171B24" };

// Runs before first paint so a saved theme never flashes. Keep it in step with
// getTheme and applyTheme in ../lib/theme.ts.
const applySavedTheme = `try{var t=localStorage.getItem("lumen-theme");document.documentElement.dataset.theme=t==="light"||t==="dark"?t:"auto"}catch(e){document.documentElement.dataset.theme="auto"}`;

// Goes first of all popstate listeners, ahead of Next's: see installNavigationGuard
// in ../lib/navigationGuard.ts.
const forwardPopState = `window.__lumenOnPopState=null;window.addEventListener("popstate",function(e){window.__lumenOnPopState&&window.__lumenOnPopState(e)},true)`;

export default function RootLayout({ children }: { children: ReactNode }) {
  return (
    <html lang="ru" suppressHydrationWarning>
      <head>
        <script dangerouslySetInnerHTML={{ __html: applySavedTheme }} />
        <script dangerouslySetInnerHTML={{ __html: forwardPopState }} />
      </head>
      <body>
        <div id="root"><ClientShell>{children}</ClientShell></div>
      </body>
    </html>
  );
}
