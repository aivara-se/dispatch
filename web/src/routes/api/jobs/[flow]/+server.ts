import { json } from '@sveltejs/kit'
import type { Card } from '$lib/types'
import { get, NotFound } from '$lib/server/record'

// One flow's jobs, for a session that cannot reach the socket. It answers the
// question a second writer asks before it pushes: is a job live on this head?
// It never caches — a lock check that reads a stale answer is worse than none.
export async function GET({ params, setHeaders }) {
  setHeaders({ 'cache-control': 'no-store' })
  try {
    const body = await get<{ jobs: Card[] }>(`/v1/flows/${encodeURIComponent(params.flow)}/jobs`)
    return json(body)
  } catch (cause) {
    if (cause instanceof NotFound) return json({ error: 'no such flow' }, { status: 404 })
    return json({ error: 'internal error' }, { status: 500 })
  }
}
