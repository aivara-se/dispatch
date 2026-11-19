import { redirect } from '@sveltejs/kit'
import { get } from '$lib/server/record'

export async function load() {
  const { flows } = await get<{ flows: { name: string }[] }>('/v1/flows')
  if (flows.length === 0) return { empty: true }
  throw redirect(302, `/flows/${encodeURIComponent(flows[0].name)}`)
}
