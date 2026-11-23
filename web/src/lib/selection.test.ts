import { expect, test } from "bun:test";
import { pick } from "./selection";

test("a selection belongs to its job", () => {
  expect(pick({ id: "212", n: 2 }, "214", 1)).toBe(1);
  expect(pick({ id: "212", n: 2 }, "212", 1)).toBe(2);
  expect(pick(null, "212", 1)).toBe(1);
});
