import { PHASE_DEVELOPMENT_SERVER } from "next/constants.js";

export default function config(phase) {
  const shared = {
    // `npm run build` runs tsc first, so Next need not check the types again.
    typescript: { ignoreBuildErrors: true },
    turbopack: { root: import.meta.dirname },
    // Project instructions live in the repository's own AGENTS.md.
    agentRules: false,
  };

  if (phase !== PHASE_DEVELOPMENT_SERVER) {
    // A static export, embedded into lumend through go:embed (ADR-0006).
    return { ...shared, output: "export", distDir: "dist" };
  }

  // The dev server proxies the API to lumend so the browser sees a single
  // origin. LUMEN_API points it at another server (make dev-prod). LUMEN_TOKEN
  // is added on the way by src/proxy.dev.ts, which only this phase loads.
  const target = process.env.LUMEN_API ?? "http://127.0.0.1:8787";
  return {
    ...shared,
    pageExtensions: ["tsx", "ts", "dev.ts"],
    async rewrites() {
      return [
        { source: "/api/:path*", destination: `${target}/api/:path*` },
        { source: "/auth/:path*", destination: `${target}/auth/:path*` },
        { source: "/healthz", destination: `${target}/healthz` },
      ];
    },
  };
}
