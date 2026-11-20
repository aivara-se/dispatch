import { json } from '@sveltejs/kit'
import type { Card } from '$lib/types'
import { get, NotFound } from '$lib/server/record'

// The live jobs on one pull request, across every flow, for a session that
// cannot reach the socket. It answers the question a second writer asks before
// it pushes: does a job own this head?
//
// It is a snapshot, not a lock. A job can start a second after it answers, so
// it narrows the race and never closes it: the engine holds one job per flow
// per agent, and that rule is the only guarantee. A session started by hand
// answers to nothing, so two of them can still land on one head.
//
// It never caches: a lock check that reads a stale "no live job" admits the
// second writer.
export async function GET({ params, setHeaders }) {
  setHeaders({ 'cache-control': 'no-store' })
  if (!/^[1-9]\d*$/.test(params.number)) {
    return json({ error: 'a pull request number' }, { status: 400 })
  }
  const ref = `${params.owner}/${params.repo}#${params.number}`

  let flows: { name: string }[]
  try {
    flows = (await get<{ flows: { name: string }[] }>('/v1/flows')).flows
  } catch {
    return json({ error: 'internal error' }, { status: 500 })
  }

  // The socket has no flat list, so every flow is read and filtered on the
  // row's own ref. The count /v1/flows carries is a separate query: a flow
  // counted at zero can gain a job before this one answers, so none is
  // skipped.
  const jobs: Card[] = []
  for (const flow of flows) {
    try {
      const body = await get<{ jobs: Card[] }>(`/v1/flows/${encodeURIComponent(flow.name)}/jobs`)
      jobs.push(...body.jobs.filter((job) => job.ref === ref && job.state === 'started'))
    } catch (cause) {
      if (cause instanceof NotFound) continue
      return json({ error: 'internal error' }, { status: 500 })
    }
  }
  return json({ ref, jobs })
}
