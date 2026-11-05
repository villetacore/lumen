import Screen from "../../../../screens/BoardPage";

export function generateStaticParams() {
  return [{ key: "_" }];
}

export default function Page() {
  return <Screen />;
}
