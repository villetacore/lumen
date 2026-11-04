import ClientRoot from "./ClientRoot";

// The export holds one page, index.html. lumend serves it for every deep link
// (see server/internal/webui), and React Router takes over in the browser.
export function generateStaticParams() {
  return [{ slug: [] }];
}

export default function Page() {
  return <ClientRoot />;
}
