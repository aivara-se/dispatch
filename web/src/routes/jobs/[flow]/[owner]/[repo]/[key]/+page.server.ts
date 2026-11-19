import { error } from '@sveltejs/kit'
import type { Job } from '$lib/types'
import { get, NotFound } from '$lib/server/record'

export async function load({ params }) {
  const { flow, owner, repo, key } = params
  try {
    const job = await get<Job>(
      `/v1/flows/${encodeURIComponent(flow)}/jobs/${encodeURIComponent(owner)}/${encodeURIComponent(repo)}/${encodeURIComponent(key)}`,
    )
    return { job }
  } catch (cause) {
    if (cause instanceof NotFound) throw error(404, 'no such job')
    throw cause
  }
}
