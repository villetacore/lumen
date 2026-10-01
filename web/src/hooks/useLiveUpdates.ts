import { useEffect } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { subscribeEvents } from "../api/client";

// useLiveUpdates invalidates every query when the vault changes on disk, so an
// edit made in Obsidian shows up here without a reload.
export function useLiveUpdates() {
  const qc = useQueryClient();
  useEffect(() => {
    let timer: number | undefined;
    const close = subscribeEvents(() => {
      // Changes arrive in bursts while a file is saved; coalesce them.
      window.clearTimeout(timer);
      timer = window.setTimeout(() => qc.invalidateQueries(), 150);
    });
    return () => {
      window.clearTimeout(timer);
      close();
    };
  }, [qc]);
}
