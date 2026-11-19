#!/usr/bin/env bun
import { homedir } from "node:os"
import { join } from "node:path"

type JobCard = {
  flow: string
  owner: string
  repo: string
  key: string
  ref: string
  title: string
  state: string
  verdict: string
  cause: string
  alert: string | null
  link: string
}

type Action = {
  type: string
  output: string | null
  exit: number | null
  skipped?: boolean
  agent?: string
  text?: string
  path?: string
  args?: string[]
  name?: string
  value?: string
}

type Task = {
  n: number
  short: string
  at: string
  actions: Action[]
  pc: unknown
  evidence: unknown
  tools: unknown
}

type Job = JobCard & { tasks: Task[] }

const usage = `usage: dispatch <command> [--socket <path>]

  flows                          each flow and its job count
  jobs [--flow F] [--state S]    one line per job: key, state, verdict, age
  show <flow>/<owner>/<repo>/<key> [--json]
  watch                          follow the change stream

Exit codes: 0 ok, 1 not found, 2 no socket or bad reply.`

let socket = join(homedir(), ".dispatch", "dispatch.sock")

function fail(message: string, code: number): never {
  console.error(message)
  process.exit(code)
}

async function get(path: string): Promise<Response> {
  try {
    return await fetch(`http://dispatch${path}`, { unix: socket })
  } catch {
    return fail(`dispatch: no socket at ${socket}`, 2)
  }
}

async function read<T>(path: string): Promise<T> {
  const response = await get(path)
  if (response.status === 404) {
    return fail("dispatch: not found", 1)
  }
  if (!response.ok) {
    return fail(`dispatch: ${response.status} ${response.statusText}`, 2)
  }
  try {
    return (await response.json()) as T
  } catch {
    return fail("dispatch: bad reply", 2)
  }
}

function age(cause: string): string {
  const seconds = Math.max(0, Math.floor((Date.now() - Date.parse(cause)) / 1000))
  if (seconds >= 86400) return `${Math.floor(seconds / 86400)}d ${Math.floor((seconds % 86400) / 3600)}h`
  if (seconds >= 3600) return `${Math.floor(seconds / 3600)}h ${Math.floor((seconds % 3600) / 60)}m`
  if (seconds >= 60) return `${Math.floor(seconds / 60)}m ${seconds % 60}s`
  return `${seconds}s`
}

function flag(args: string[], name: string): string | undefined {
  const index = args.indexOf(name)
  if (index === -1) return undefined
  return args[index + 1]
}

async function flows(): Promise<void> {
  const body = await read<{ flows: { name: string; jobs: number }[] }>("/v1/flows")
  for (const flow of body.flows) {
    console.log(`${flow.name} ${flow.jobs}`)
  }
}

async function jobs(args: string[]): Promise<void> {
  const flow = flag(args, "--flow")
  const state = flag(args, "--state")
  const names = flow ? [flow] : (await read<{ flows: { name: string }[] }>("/v1/flows")).flows.map((item) => item.name)
  for (const name of names) {
    const body = await read<{ jobs: JobCard[] }>(`/v1/flows/${encodeURIComponent(name)}/jobs`)
    for (const job of body.jobs) {
      if (state && job.state !== state) continue
      console.log(`${job.key} ${job.state} ${job.verdict} ${age(job.cause)}`)
    }
  }
}

async function show(args: string[]): Promise<void> {
  const target = args.find((arg) => !arg.startsWith("--") && arg !== "json")
  if (!target) fail(usage, 2)
  const parts = target.split("/")
  if (parts.length !== 4) fail(usage, 2)
  const [flow, owner, repo, key] = parts
  const job = await read<Job>(`/v1/flows/${encodeURIComponent(flow)}/jobs/${encodeURIComponent(owner)}/${encodeURIComponent(repo)}/${encodeURIComponent(key)}`)
  if (args.includes("--json")) {
    console.log(JSON.stringify(job, null, 2))
    return
  }
  console.log(`${job.ref} ${job.title}`)
  console.log(`state ${job.state} verdict ${job.verdict}`)
  if (job.alert) console.log(`alert ${job.alert}`)
  console.log(job.link)
  for (const task of job.tasks) {
    console.log(`task ${task.n} ${task.short} ${task.at}`)
    for (const action of task.actions) {
      const output = action.skipped ? "skipped" : (action.output ?? "").replace(/\s+/g, " ").trim()
      const exit = action.exit === null ? "-" : String(action.exit)
      console.log(`  ${action.type} exit=${exit} ${output}`)
    }
  }
}

async function watch(): Promise<void> {
  const response = await get("/v1/stream")
  if (!response.ok || !response.body) fail("dispatch: bad stream", 2)
  process.on("SIGINT", () => process.exit(0))
  const reader = response.body.getReader()
  const decoder = new TextDecoder()
  let buffer = ""
  while (true) {
    const { value, done } = await reader.read()
    if (done) return
    buffer += decoder.decode(value, { stream: true })
    let index = buffer.indexOf("\n\n")
    while (index !== -1) {
      const block = buffer.slice(0, index)
      buffer = buffer.slice(index + 2)
      for (const line of block.split("\n")) {
        if (!line.startsWith("data: ")) continue
        const event = JSON.parse(line.slice(6)) as { flow: string; owner: string; repo: string; key: string }
        console.log(`${event.flow} ${event.owner}/${event.repo} ${event.key}`)
      }
      index = buffer.indexOf("\n\n")
    }
  }
}

const argv = process.argv.slice(2)
const rest: string[] = []
for (let i = 0; i < argv.length; i++) {
  if (argv[i] === "--socket") {
    const next = argv[++i]
    if (!next) fail("dispatch: --socket needs a path", 2)
    socket = next
  } else {
    rest.push(argv[i])
  }
}

const command = rest[0]
const args = rest.slice(1)
switch (command) {
  case "flows":
    await flows()
    break
  case "jobs":
    await jobs(args)
    break
  case "show":
    await show(args)
    break
  case "watch":
    await watch()
    break
  default:
    fail(usage, 2)
}

process.exit(0)
