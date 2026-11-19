import { afterAll, beforeAll, expect, test } from "bun:test"
import { spawn, type ChildProcess } from "node:child_process"
import { rmSync } from "node:fs"
import { tmpdir } from "node:os"
import { join } from "node:path"

const socket = join(tmpdir(), `dispatch-web-${process.pid}.sock`)
const root = join(import.meta.dir, "..")
const port = 3417
const cause = new Date(Date.now() - 7200_000).toISOString()

const card = (key: string, state: string, verdict: string, title: string) => ({
  flow: "review",
  owner: "aivara-se",
  repo: "dispatch",
  key,
  ref: `aivara-se/dispatch#${key}`,
  title,
  state,
  verdict,
  cause,
  alert: null,
  link: `https://github.com/aivara-se/dispatch/pull/${key}`,
})

const detail = {
  ...card("212", "blocked", "failed", "review the engine spine"),
  alert: "the label write failed",
  tasks: [
    {
      n: 1,
      short: "read the record",
      at: "2026-11-19T00:00:00Z",
      actions: [{ type: "prompt", agent: "momo", output: "looks good", exit: 0 }],
      pc: {},
      evidence: {},
      tools: [],
    },
    {
      n: 2,
      short: "label the pull request",
      at: "2026-11-19T00:05:00Z",
      actions: [
        { type: "action", name: "label", value: "blocked", output: null, exit: 1 },
        { type: "prompt", agent: "meme", output: null, exit: null, skipped: true },
      ],
      pc: {},
      evidence: {},
      tools: [],
    },
  ],
}

const cards = [
  card("212", "blocked", "failed", "review the engine spine"),
  card("214", "started", "pending", "audit the socket"),
  card("210", "done", "passed", "land the cli"),
]

rmSync(socket, { force: true })
const api = Bun.serve({
  unix: socket,
  fetch(request) {
    const path = new URL(request.url).pathname
    if (path === "/v1/flows") return Response.json({ flows: [{ name: "review", jobs: 3 }, { name: "audit", jobs: 0 }] })
    if (path === "/v1/flows/review/jobs") return Response.json({ jobs: cards })
    if (path === "/v1/flows/audit/jobs") return Response.json({ jobs: [] })
    if (path === "/v1/flows/review/jobs/aivara-se/dispatch/212") return Response.json(detail)
    if (path === "/v1/stream") {
      const body = new ReadableStream({
        start(controller) {
          controller.enqueue(new TextEncoder().encode(`data: {"flow":"review","owner":"aivara-se","repo":"dispatch","key":"212"}\n\n`))
        },
      })
      return new Response(body, { headers: { "content-type": "text/event-stream" } })
    }
    return new Response("not found", { status: 404 })
  },
})

let web: ChildProcess
let errors = ""

beforeAll(async () => {
  web = spawn("node", ["build"], {
    cwd: root,
    env: { ...process.env, DISPATCH_SOCKET: socket, PORT: String(port), ORIGIN: `http://127.0.0.1:${port}` },
    stdio: "pipe",
  })
  web.stderr?.on("data", (chunk) => (errors += chunk))
  for (let i = 0; i < 100; i++) {
    try {
      const response = await fetch(`http://127.0.0.1:${port}/flows/review`)
      if (response.ok) return
    } catch {
      // not up yet
    }
    await Bun.sleep(100)
  }
  throw new Error(`the web server did not start\n${errors}`)
}, 30_000)

afterAll(() => {
  web?.kill()
  api.stop(true)
  rmSync(socket, { force: true })
})

test("the server renders the record before hydration", async () => {
  const html = await (await fetch(`http://127.0.0.1:${port}/jobs/review/aivara-se/dispatch/212`)).text()
  expect(html).toContain("review the engine spine")
  expect(html).toContain("read the record")
  expect(html).toContain("failed · exit 1")
  expect(html).toContain("skipped")
  expect(html).toContain("the label write failed")
})

test("a blocked job opens on its failing task", async () => {
  const html = await (await fetch(`http://127.0.0.1:${port}/jobs/review/aivara-se/dispatch/212`)).text()
  expect(html).toContain("Task 2")
  expect(html).not.toContain("<pre>looks good</pre>")
})

test("the flow page lists its jobs", async () => {
  const html = await (await fetch(`http://127.0.0.1:${port}/flows/review`)).text()
  expect(html).toContain("aivara-se/dispatch#212")
  expect(html).toContain("audit the socket")
})

test("an unknown job is 404", async () => {
  const response = await fetch(`http://127.0.0.1:${port}/jobs/review/aivara-se/dispatch/999`)
  expect(response.status).toBe(404)
})

test("the stream is proxied", async () => {
  const response = await fetch(`http://127.0.0.1:${port}/api/stream`)
  const reader = response.body!.getReader()
  const { value } = await reader.read()
  await reader.cancel()
  expect(new TextDecoder().decode(value)).toContain('"key":"212"')
})
