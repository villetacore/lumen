import { useEffect, useId, useRef, useState } from "react";

/**
 * MermaidBlock draws a ```mermaid fence. The library is loaded only when a
 * page actually contains a diagram. It runs with securityLevel "strict":
 * labels are SVG text, click handlers and scripts are dropped, and the SVG
 * is sanitized by Mermaid before it reaches the page. That SVG is the one
 * place Markdown output is inserted as markup.
 */
export default function MermaidBlock({ code }: { code: string }) {
  const id = "mmd-" + useId().replace(/[^a-zA-Z0-9]/g, "");
  const host = useRef<HTMLDivElement>(null);
  const dark = useDarkTheme();
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    setError(null);
    (async () => {
      try {
        const { default: mermaid } = await import("mermaid");
        mermaid.initialize({
          startOnLoad: false,
          securityLevel: "strict",
          // HTML labels survive sanitizing as <img> and the like, which would
          // let a note load a tracking pixel. SVG text labels cannot.
          htmlLabels: false,
          flowchart: { htmlLabels: false },
          theme: dark ? "dark" : "default",
          fontFamily: "inherit",
        });
        const { svg } = await mermaid.render(id, code);
        if (!cancelled && host.current) host.current.innerHTML = svg;
      } catch (e) {
        // Mermaid leaves an error SVG behind in <body> on failure.
        document.getElementById("d" + id)?.remove();
        if (!cancelled) setError(e instanceof Error ? e.message : String(e));
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [code, dark, id]);

  if (error) {
    return (
      <div className="md-mermaid md-mermaid--error" role="group" aria-label="Ошибка схемы">
        <p className="md-mermaid__error">Схема не построена: {error}</p>
        <pre className="md-code">
          <code>{code}</code>
        </pre>
      </div>
    );
  }
  return <div ref={host} className="md-mermaid" role="img" aria-label="Схема Mermaid" />;
}

function useDarkTheme(): boolean {
  const read = () => {
    const theme = document.documentElement.dataset.theme;
    if (theme === "dark") return true;
    if (theme === "light") return false;
    return window.matchMedia?.("(prefers-color-scheme: dark)").matches ?? false;
  };
  const [dark, setDark] = useState(read);
  useEffect(() => {
    const update = () => setDark(read());
    const observer = new MutationObserver(update);
    observer.observe(document.documentElement, { attributes: true, attributeFilter: ["data-theme"] });
    const media = window.matchMedia?.("(prefers-color-scheme: dark)");
    media?.addEventListener("change", update);
    return () => {
      observer.disconnect();
      media?.removeEventListener("change", update);
    };
  }, []);
  return dark;
}
