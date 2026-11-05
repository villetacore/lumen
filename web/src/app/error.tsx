"use client";

import CrashScreen from "../components/CrashScreen";

export default function Error({ error }: { error: Error }) {
  return <CrashScreen error={error} />;
}
