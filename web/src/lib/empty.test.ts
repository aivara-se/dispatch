import { expect, test } from "bun:test";
import { emptyLine } from "./empty";

test("an empty record and an empty search say different things", () => {
  expect(emptyLine(0, 0)).toBe("No jobs yet.");
  expect(emptyLine(4, 0)).toBe("No pull request matches.");
  expect(emptyLine(4, 2)).toBeNull();
});
