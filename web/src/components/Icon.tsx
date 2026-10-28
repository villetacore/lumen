/** Small, consistent local icon set; no font or network dependency. */
const paths = {
  sun: "M12 8a4 4 0 1 0 0 8 4 4 0 0 0 0-8ZM12 2v2m0 16v2M2 12h2m16 0h2M5 5l1.5 1.5m11 11L19 19M5 19l1.5-1.5m11-11L19 5",
  inbox: "M4 4h16l2 12v4H2v-4L4 4ZM2 15h6l2 3h4l2-3h6",
  calendar: "M4 5h16v16H4V5Zm3-3v6m10-6v6M4 11h16m-12 4h2m4 0h2",
  alert: "m12 3 10 18H2L12 3Zm0 6v5m0 3v.1",
  arrow: "M4 12h16m-6-6 6 6-6 6",
  clock: "M12 3a9 9 0 1 0 0 18 9 9 0 0 0 0-18Zm0 4v5l3 2",
  list: "M9 6h12M9 12h12M9 18h12M3 6h1m-1 6h1m-1 6h1",
  check: "m5 12 4 4L19 6",
  folder: "M3 6h7l2 2h9v12H3V6Z",
  plus: "M12 5v14M5 12h14",
  file: "M5 3h9l5 5v13H5V3Zm9 0v6h5M8 13h8m-8 4h6",
  book: "M12 5C8 3 5 3 2 4v15c3-1 6-1 10 1 4-2 7-2 10-1V4c-3-1-6-1-10 1Zm0 0v15",
  plug: "m8 3 0 5m8-5v5M5 8h14v3a7 7 0 0 1-14 0V8Zm7 10v4",
  key: "M8 3a5 5 0 1 0 0 10 5 5 0 0 0 0-10Zm4 9 9 9m-3-3 3-3m-6 0 3-3",
  hash: "m9 3-3 18M18 3l-3 18M3 9h19M2 15h19",
  search: "M10.5 3a7.5 7.5 0 1 0 0 15 7.5 7.5 0 0 0 0-15ZM16 16l5 5",
  refresh: "M20 8a9 9 0 1 0 0 8M20 3v5h-5",
  lock: "M5 10h14v11H5V10Zm3 0V6a4 4 0 0 1 8 0v4m-4 5v2",
  spark: "m12 2 2.5 7.5L22 12l-7.5 2.5L12 22l-2.5-7.5L2 12l7.5-2.5L12 2Z",
} as const;
export type IconName = keyof typeof paths;
export default function Icon({ name, size = 18, className = "" }: { name: IconName; size?: number; className?: string }) {
  return <svg className={`ui-icon ${className}`} width={size} height={size} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.65" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true"><path d={paths[name]} /></svg>;
}
