import Screen from "../../../screens/IssuePage";

export function generateStaticParams() {
  return [{ key: "_" }];
}

export default function Page() {
  return <Screen />;
}
