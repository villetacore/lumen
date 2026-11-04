"use client";

import "@villetacore/velessoft-tokens/tokens.css";
import "@villetacore/velessoft-ui/styles.css";
import "../styles.css";
import CrashScreen from "../components/CrashScreen";

export default function GlobalError({ error }: { error: Error }) {
  return <html lang="ru"><body><CrashScreen error={error} /></body></html>;
}
