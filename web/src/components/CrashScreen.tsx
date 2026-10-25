import { useRouteError } from "react-router-dom";

/** CrashScreen replaces a blank page when rendering fails. Files on disk are untouched. */
export default function CrashScreen() {
  const err = useRouteError();
  return (
    <div className="empty" style={{ maxWidth: 560, margin: "12vh auto", textAlign: "left" }}>
      <h2 style={{ color: "var(--text)" }}>Интерфейс споткнулся</h2>
      <p>
        Данные в vault не пострадали — это ошибка отображения. Перезагрузите страницу; если
        повторяется, приложите текст ниже к отчёту.
      </p>
      <pre className="md-code" style={{ whiteSpace: "pre-wrap" }}>
        {err instanceof Error ? err.message : String(err)}
      </pre>
      <button className="btn primary" onClick={() => window.location.reload()}>
        Перезагрузить
      </button>{" "}
      <a className="btn" href="/">
        На главную
      </a>
    </div>
  );
}
