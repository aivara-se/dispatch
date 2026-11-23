import { describe, expect, test } from "bun:test";
import { toolCalls } from "./tools";

describe("toolCalls", () => {
  test("reads a function call's name and arguments", () => {
    const got = toolCalls([
      {
        id: "c1",
        type: "function",
        function: { name: "read_file", arguments: '{"path":"a.go"}' },
      },
    ]);
    expect(got.total).toBe(1);
    expect(got.calls[0]?.name).toBe("read_file");
    expect(got.calls[0]?.detail).toBe('{"path":"a.go"}');
  });

  test("reads a bare name and leaves the detail empty", () => {
    const got = toolCalls([{ name: "read" }]);
    expect(got.calls[0]).toEqual({ name: "read", detail: "" });
  });

  test("keeps a call with no name readable", () => {
    const got = toolCalls([{ arguments: "{}" }]);
    expect(got.calls[0]?.name).toBe("call");
  });

  test("returns nothing for a task that recorded none", () => {
    for (const raw of [null, undefined, [], "", {}, 3])
      expect(toolCalls(raw)).toEqual({ calls: [], total: 0, more: 0 });
  });

  test("counts what it leaves out", () => {
    const raw = Array.from({ length: 30 }, (_, i) => ({ name: `tool_${i}` }));
    const got = toolCalls(raw, 12);
    expect(got.calls).toHaveLength(12);
    expect(got.total).toBe(30);
    expect(got.more).toBe(18);
  });

  test("clips a long argument list", () => {
    const got = toolCalls([{ name: "run", arguments: "x".repeat(400) }]);
    expect(got.calls[0]!.detail.length).toBe(120);
    expect(got.calls[0]!.detail.endsWith("…")).toBe(true);
  });
});
