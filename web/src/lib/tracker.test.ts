import { expect, test } from "vitest";
import { suggestKey } from "./tracker";

test("a project key comes from its name", () => {
  expect(suggestKey("Сайт студии")).toBe("SS");
  expect(suggestKey("Сайт")).toBe("SAJT");
  expect(suggestKey("Website redesign 2")).toBe("WR2");
  expect(suggestKey("Lumen")).toBe("LUME");
  expect(suggestKey("Большой длинный проект для всей нашей студии целиком сразу")).toBe("BDPDVNSCS");
});

test("no key is suggested from what cannot start one", () => {
  expect(suggestKey("")).toBe("");
  expect(suggestKey("  ")).toBe("");
  expect(suggestKey("2026")).toBe("");
});
