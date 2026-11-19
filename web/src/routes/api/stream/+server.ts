import { Readable } from 'node:stream'
import { stream } from '$lib/server/record'

export async function GET() {
  const upstream = await stream('/v1/stream')
  return new Response(Readable.toWeb(upstream) as unknown as ReadableStream, {
    headers: {
      'content-type': 'text/event-stream',
      'cache-control': 'no-cache',
      connection: 'keep-alive',
    },
  })
}
