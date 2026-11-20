export function emptyLine(total: number, shown: number): string | null {
  if (shown > 0) return null
  return total === 0 ? 'No jobs yet.' : 'No pull request matches.'
}
