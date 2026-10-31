import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

// The dev server proxies /api to lumend so the browser sees a single origin.
// LUMEN_API points it at another server (make dev-prod); LUMEN_TOKEN is then
// added to every proxied request, so the token never reaches the browser.
const target = process.env.LUMEN_API ?? "http://127.0.0.1:8787";
const token = process.env.LUMEN_TOKEN;
const headers = token ? { Authorization: `Bearer ${token}` } : undefined;

export default defineConfig({
  plugins: [react()],
  server: {
    port: 5173,
    proxy: {
      "/api": { target, changeOrigin: true, headers },
      "/healthz": { target, changeOrigin: true },
    },
  },
});
