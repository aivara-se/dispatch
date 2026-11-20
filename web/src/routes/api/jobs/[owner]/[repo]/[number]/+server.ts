import { json } from '@sveltejs/kit'
import type { Card } from '$lib/types'
import { get, NotFound } from '$lib/server/record'

// The jobs on one pull request, across every flow, for a session that cannot
// reach the socket. It answers the question a second writer asks before it
// pushes: does a job own this head? It never caches — a lock check that reads
// a stale answer is worse than none — and it is a snapshot: a job can start a
// second after it answers, so it narrows the window and never closes it.
export async function GET({ params, setHeaders }) {
  setHeaders({ 'cache-control': 'no-store' })
  const number = Number(params.number)
  if (!Number.isInteger(number) || number < 1) {
    return json({ error: 'a pull request number' }, { status: 400 })
  }
  const ref = `${params.owner}/${params.repo}#${number}`

  let flows: { name: string; jobs: number }[]
  try {
    flows = (await get<{ flows: { name: string; jobs: number }[] }>('/v1/flows')).flows
  } catch {
    return json({ error: 'internal error' }, { status: 500 })
  }

  // A flow with no jobs cannot hold this one, and the socket has no flat list,
  // so every flow with a count is read and filtered on the row's own ref.
  const jobs: Card[] = []
  for (const flow of flows) {
    if (flow.jobs === 0) continue
    try {
      const body = await get<{ jobs: Card[] }>(`/v1/flows/${encodeURIComponent(flow.name)}/jobs`)
      jobs.push(...body.jobs.filter((job) => job.ref === ref))
    } catch (cause) {
      if (cause instanceof NotFound) continue
      return json({ error: 'internal error' }, { status: 500 })
    }
  }
  return json({ ref, jobs })
}
