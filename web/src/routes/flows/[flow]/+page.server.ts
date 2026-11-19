import { error } from '@sveltejs/kit'
import type { Card } from '$lib/types'
import { get, NotFound } from '$lib/server/record'

export async function load({ params }) {
  try {
    const { jobs } = await get<{ jobs: Card[] }>(`/v1/flows/${encodeURIComponent(params.flow)}/jobs`)
    return { flow: params.flow, jobs }
  } catch (cause) {
    if (cause instanceof NotFound) throw error(404, 'no such flow')
    throw cause
  }
}
