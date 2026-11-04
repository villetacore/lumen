import type { Metadata, Viewport } from "next";
import type { ReactNode } from "react";
import "@villetacore/velessoft-tokens/tokens.css";
import "@villetacore/velessoft-ui/styles.css";
import "../styles.css";

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

export default function RootLayout({ children }: { children: ReactNode }) {
  return (
    <html lang="ru" suppressHydrationWarning>
      <head>
        <script dangerouslySetInnerHTML={{ __html: applySavedTheme }} />
      </head>
      <body>
        <div id="root">{children}</div>
      </body>
    </html>
  );
}
