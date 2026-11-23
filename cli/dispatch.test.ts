import { afterAll, expect, test } from "bun:test";
import { rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

const socket = join(tmpdir(), `dispatch-cli-${process.pid}.sock`);
const root = join(import.meta.dir, "..");
const cause = new Date(Date.now() - 3600_000).toISOString();

const card = (key: string, state: string, verdict: string) => ({
  flow: "review",
  owner: "aivara-se",
  repo: "dispatch",
  key,
  ref: `aivara-se/dispatch#${key}`,
  title: "a title",
  state,
  verdict,
  cause,
  alert: null,
  link: `https://github.com/aivara-se/dispatch/pull/${key}`,
});

const detail = {
  ...card("212", "blocked", "failed"),
  alert: "the job stopped",
  tasks: [
    {
      n: 1,
      short: "review",
      at: "2026-11-19T00:00:00Z",
      actions: [
        { type: "prompt", agent: "momo", output: "reviewed", exit: 0 },
        {
          type: "script",
          path: "check.sh",
          output: null,
          exit: null,
          skipped: true,
        },
      ],
      pc: { expr: "pr.approved", want: true, got: false },
      evidence: { head: "s1" },
      tools: [],
    },
  ],
};

rmSync(socket, { force: true });
const server = Bun.serve({
  unix: socket,
  fetch(request) {
    const path = new URL(request.url).pathname;
    if (path === "/v1/flows") {
      return Response.json({
        flows: [
          { name: "review", jobs: 2 },
          { name: "audit", jobs: 0 },
        ],
      });
    }
    if (path === "/v1/flows/review/jobs") {
      return Response.json({
        jobs: [
          card("212", "blocked", "failed"),
          card("214", "started", "pending"),
        ],
      });
    }
    if (path === "/v1/flows/audit/jobs") {
      return Response.json({ jobs: [] });
    }
    if (path === "/v1/flows/review/jobs/aivara-se/dispatch/212") {
      return Response.json(detail);
    }
    if (path === "/v1/stream") {
      const body = new ReadableStream({
        start(controller) {
          const encoder = new TextEncoder();
          controller.enqueue(
            encoder.encode(
              `data: {"flow":"review","owner":"aivara-se","repo":"dispatch","key":"212"}\n\n` +
                `data: {"flow":"review","owner":"aivara-se","repo":"dispatch","key":"213"}\n\n`,
            ),
          );
        },
      });
      return new Response(body, {
        headers: { "Content-Type": "text/event-stream" },
      });
    }
    return new Response("not found", { status: 404 });
  },
});

afterAll(() => {
  server.stop(true);
  rmSync(socket, { force: true });
});

async function cli(args: string[], path = socket) {
  const child = Bun.spawn({
    cmd: ["bun", "cli/dispatch.ts", ...args, "--socket", path],
    cwd: root,
    stdout: "pipe",
    stderr: "pipe",
  });
  const code = await child.exited;
  return {
    code,
    out: await new Response(child.stdout).text(),
    err: await new Response(child.stderr).text(),
  };
}

test("flows prints each flow and its count", async () => {
  const result = await cli(["flows"]);
  expect(result.code).toBe(0);
  expect(result.out).toBe("review 2\naudit 0\n");
});

test("jobs prints one line per job", async () => {
  const result = await cli(["jobs"]);
  expect(result.code).toBe(0);
  const lines = result.out.trim().split("\n");
  expect(lines).toHaveLength(2);
  expect(lines[0]).toMatch(/^212 blocked failed \d+[dhms]/);
  expect(lines[1]).toMatch(/^214 started pending \d+[dhms]/);
});

test("jobs filters by flow and state", async () => {
  const result = await cli(["jobs", "--flow", "review", "--state", "started"]);
  expect(result.code).toBe(0);
  expect(result.out.trim().split("\n")).toHaveLength(1);
  expect(result.out).toContain("214");
});

test("show prints the job, its tasks and the outputs", async () => {
  const result = await cli(["show", "review/aivara-se/dispatch/212"]);
  expect(result.code).toBe(0);
  expect(result.out).toContain("aivara-se/dispatch#212 a title");
  expect(result.out).toContain("state blocked verdict failed");
  expect(result.out).toContain("alert the job stopped");
  expect(result.out).toContain("task 1 review");
  expect(result.out).toContain("prompt exit=0 reviewed");
  expect(result.out).toContain("script exit=- skipped");
});

test("show --json prints the API object", async () => {
  const result = await cli(["show", "review/aivara-se/dispatch/212", "--json"]);
  expect(result.code).toBe(0);
  const job = JSON.parse(result.out);
  expect(job.key).toBe("212");
  expect(job.tasks[0].actions[0].output).toBe("reviewed");
});

test("a missing job exits 1", async () => {
  const result = await cli(["show", "review/aivara-se/dispatch/999"]);
  expect(result.code).toBe(1);
});

test("a missing socket exits 2", async () => {
  const result = await cli(
    ["flows"],
    join(tmpdir(), `missing-${process.pid}.sock`),
  );
  expect(result.code).toBe(2);
  expect(result.err).toContain("no socket");
});

test("watch prints each event and exits on SIGINT", async () => {
  const child = Bun.spawn({
    cmd: ["bun", "cli/dispatch.ts", "watch", "--socket", socket],
    cwd: root,
    stdout: "pipe",
    stderr: "pipe",
  });
  await Bun.sleep(400);
  child.kill("SIGINT");
  const code = await child.exited;
  const out = await new Response(child.stdout).text();
  expect(out).toContain("review aivara-se/dispatch 212");
  expect(out).toContain("review aivara-se/dispatch 213");
  expect(code).toBe(0);
});
