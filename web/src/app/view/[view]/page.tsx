import Screen from "../../../screens/TasksPage";

export function generateStaticParams() {
  return [{ view: "_" }];
}

export default function Page() {
  return <Screen />;
}
