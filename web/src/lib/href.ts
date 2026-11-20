import type { Card } from './types'

export const jobHref = (job: Pick<Card, 'flow' | 'owner' | 'repo' | 'key'>) =>
  `/jobs/${encodeURIComponent(job.flow)}/${encodeURIComponent(job.owner)}/${encodeURIComponent(job.repo)}/${encodeURIComponent(job.key)}`
