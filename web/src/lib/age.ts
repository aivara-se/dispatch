const pad = (value: number) => String(value).padStart(2, '0')

/** The hour and minute of a record timestamp, in the lab's time zone. */
export function hhmm(at: string): string {
  return new Intl.DateTimeFormat('en-GB', {
    hour: '2-digit',
    minute: '2-digit',
    hour12: false,
    timeZone: 'Europe/Stockholm',
  }).format(new Date(at))
}

export function age(cause: string, now = Date.now()): string {
  const seconds = Math.max(0, Math.floor((now - Date.parse(cause)) / 1000))
  if (seconds >= 86400) return `${Math.floor(seconds / 86400)}d ${Math.floor((seconds % 86400) / 3600)}h`
  if (seconds >= 3600) return `${Math.floor(seconds / 3600)}h ${pad(Math.floor((seconds % 3600) / 60))}m`
  if (seconds >= 60) return `${Math.floor(seconds / 60)}m ${pad(seconds % 60)}s`
  return `${seconds}s`
}
