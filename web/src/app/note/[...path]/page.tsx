import Screen from "../../../screens/NotePage";

export function generateStaticParams() {
  return [{ path: ["_"] }];
}

export default function Page() {
  return <Screen />;
}
