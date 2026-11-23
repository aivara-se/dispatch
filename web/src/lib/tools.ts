// A task's tool calls are stored as the session's own JSON. The page shows
// what an agent called, which is the evidence a report alone cannot give, so
// this reads the shapes Hermes writes and falls back to whatever it finds.
export type ToolCall = { name: string; detail: string };

const CLIP = 120;

const text = (value: unknown): string => {
  if (value === undefined || value === null) return "";
  if (typeof value === "string") return value;
  try {
    return JSON.stringify(value) ?? "";
  } catch {
    return "";
  }
};

const clip = (value: string) =>
  value.length > CLIP ? `${value.slice(0, CLIP - 1)}…` : value;

const named = (value: unknown): string | null =>
  typeof value === "string" && value !== "" ? value : null;

const nameOf = (call: Record<string, unknown>): string => {
  const fn = (call.function ?? call) as Record<string, unknown>;
  return named(fn.name) ?? named(call.tool) ?? named(call.type) ?? "call";
};

const detailOf = (call: Record<string, unknown>): string => {
  const fn = (call.function ?? call) as Record<string, unknown>;
  for (const key of ["arguments", "args", "input", "parameters"]) {
    const detail = clip(text(fn[key]));
    if (detail !== "") return detail;
  }
  return "";
};

// toolCalls reads a task's tools field. It returns the first `limit` calls and
// how many it left out, so a task with a hundred calls is still one screen.
export function toolCalls(
  raw: unknown,
  limit = 12,
): { calls: ToolCall[]; total: number; more: number } {
  if (!Array.isArray(raw)) return { calls: [], total: 0, more: 0 };
  const all = raw.flatMap((call) =>
    call !== null && typeof call === "object"
      ? [
          {
            name: nameOf(call as Record<string, unknown>),
            detail: detailOf(call as Record<string, unknown>),
          },
        ]
      : [],
  );
  return {
    calls: all.slice(0, limit),
    total: all.length,
    more: Math.max(0, all.length - limit),
  };
}
