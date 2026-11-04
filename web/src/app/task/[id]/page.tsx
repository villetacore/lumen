import Screen from "../../../screens/TaskPage";

export function generateStaticParams() {
  return [{ id: "_" }];
}

export default function Page() {
  return <Screen />;
}
