import { request, type IncomingMessage } from 'node:http'
import { homedir } from 'node:os'
import { join } from 'node:path'

const socket = process.env.DISPATCH_SOCKET ?? join(homedir(), '.dispatch', 'dispatch.sock')

export class NotFound extends Error {}

export function get<T>(path: string): Promise<T> {
  return new Promise((resolve, reject) => {
    const req = request(
      { socketPath: socket, path, method: 'GET', headers: { accept: 'application/json' } },
      (res) => {
        const chunks: Buffer[] = []
        res.on('data', (chunk) => chunks.push(chunk as Buffer))
        res.on('end', () => {
          const status = res.statusCode ?? 0
          if (status === 404) return reject(new NotFound())
          if (status < 200 || status >= 300) return reject(new Error(`record: ${status}`))
          try {
            resolve(JSON.parse(Buffer.concat(chunks).toString('utf8')) as T)
          } catch {
            reject(new Error('record: bad reply'))
          }
        })
      },
    )
    req.on('error', reject)
    req.end()
  })
}

export function stream(path: string): Promise<IncomingMessage> {
  return new Promise((resolve, reject) => {
    const req = request(
      { socketPath: socket, path, method: 'GET', headers: { accept: 'text/event-stream' } },
      (res) => {
        const status = res.statusCode ?? 0
        if (status < 200 || status >= 300) {
          res.resume()
          return reject(new Error(`record: ${status}`))
        }
        resolve(res)
      },
    )
    req.on('error', reject)
    req.end()
  })
}
