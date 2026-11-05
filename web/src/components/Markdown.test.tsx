// @vitest-environment jsdom
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, expect, test, vi } from "vitest";
import Markdown from "./Markdown";

const mermaid = vi.hoisted(() => ({ initialize: vi.fn(), render: vi.fn() }));
vi.mock("mermaid", () => ({ default: mermaid }));

beforeEach(() => {
  mermaid.initialize.mockReset();
  mermaid.render.mockReset();
});
afterEach(cleanup);

function show(body: string) {
  return render(<Markdown body={body} />);
}

test("mermaid fence is drawn in strict mode", async () => {
  mermaid.render.mockResolvedValue({ svg: '<svg data-testid="diagram"><text>A</text></svg>' });
  show("Текст\n\n```mermaid\nflowchart LR\n  A --> B\n```\n");
  expect(await screen.findByTestId("diagram")).toBeTruthy();
  expect(mermaid.initialize).toHaveBeenCalledWith(expect.objectContaining({ securityLevel: "strict", startOnLoad: false, htmlLabels: false }));
  expect(mermaid.render.mock.calls[0][1]).toBe("flowchart LR\n  A --> B");
});

test("broken diagram shows the error next to its source", async () => {
  mermaid.render.mockRejectedValue(new Error("Parse error on line 1"));
  show("```mermaid\nне схема\n```");
  expect(await screen.findByText(/Схема не построена: Parse error on line 1/)).toBeTruthy();
  expect(screen.getByText("не схема")).toBeTruthy();
});

test("other fences stay plain code and do not load mermaid", async () => {
  const { container } = show("```js\nconst x = '<img src=x onerror=alert(1)>';\n```\n\n```\nmermaid\n```");
  await waitFor(() => expect(container.querySelectorAll("pre.md-code").length).toBe(2));
  expect(container.querySelector("img")).toBeNull();
  expect(mermaid.render).not.toHaveBeenCalled();
});
