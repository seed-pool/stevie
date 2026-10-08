const USE_LOCAL_KEY = 'stevie.useLocalTime'
const TIMEZONE_KEY = 'stevie.timezone'

/** Curated IANA zones for the Settings dropdown. */
export const TIMEZONE_OPTIONS: { value: string; label: string }[] = [
  { value: 'UTC', label: 'UTC' },
  { value: 'America/New_York', label: 'Eastern (America/New_York)' },
  { value: 'America/Chicago', label: 'Central (America/Chicago)' },
  { value: 'America/Denver', label: 'Mountain (America/Denver)' },
  { value: 'America/Los_Angeles', label: 'Pacific (America/Los_Angeles)' },
  { value: 'America/Toronto', label: 'Toronto' },
  { value: 'America/Vancouver', label: 'Vancouver' },
  { value: 'America/Sao_Paulo', label: 'São Paulo' },
  { value: 'America/Argentina/Buenos_Aires', label: 'Buenos Aires' },
  { value: 'Europe/London', label: 'London' },
  { value: 'Europe/Paris', label: 'Paris' },
  { value: 'Europe/Berlin', label: 'Berlin' },
  { value: 'Europe/Madrid', label: 'Madrid' },
  { value: 'Asia/Dubai', label: 'Dubai' },
  { value: 'Asia/Kolkata', label: 'India' },
  { value: 'Asia/Singapore', label: 'Singapore' },
  { value: 'Asia/Tokyo', label: 'Tokyo' },
  { value: 'Australia/Sydney', label: 'Sydney' },
  { value: 'Pacific/Auckland', label: 'Auckland' },
]

export type TimePrefs = {
  useLocalTime: boolean
  timeZone: string
}

export function detectBrowserTimeZone(): string {
  try {
    return Intl.DateTimeFormat().resolvedOptions().timeZone || 'UTC'
  } catch {
    return 'UTC'
  }
}

export function getTimePrefs(): TimePrefs {
  let useLocalTime = false
  let timeZone = detectBrowserTimeZone()
  try {
    useLocalTime = localStorage.getItem(USE_LOCAL_KEY) === '1'
    const stored = localStorage.getItem(TIMEZONE_KEY)
    if (stored) timeZone = stored
  } catch {
    /* ignore */
  }
  return { useLocalTime, timeZone }
}

export function setUseLocalTime(on: boolean) {
  try {
    localStorage.setItem(USE_LOCAL_KEY, on ? '1' : '0')
  } catch {
    /* ignore */
  }
}

export function setTimeZone(tz: string) {
  try {
    localStorage.setItem(TIMEZONE_KEY, tz)
  } catch {
    /* ignore */
  }
}

/** Effective IANA zone for display. */
export function effectiveTimeZone(prefs: TimePrefs): string {
  if (!prefs.useLocalTime) return 'UTC'
  return prefs.timeZone || detectBrowserTimeZone() || 'UTC'
}

function zoneAbbr(date: Date, timeZone: string): string {
  try {
    const parts = new Intl.DateTimeFormat('en-US', {
      timeZone,
      timeZoneName: 'short',
    }).formatToParts(date)
    return parts.find((p) => p.type === 'timeZoneName')?.value || timeZone
  } catch {
    return timeZone === 'UTC' ? 'UTC' : timeZone
  }
}

export type FormatWhenOpts = {
  /** Include weekday + month + day */
  date?: boolean
  /** Include seconds */
  seconds?: boolean
}

/** Format an ISO/instant for UI: e.g. "11:30 PM UTC" or "Wednesday, October 7, 7:00 PM EDT". */
export function formatWhen(isoOrDate: string | Date | number, prefs: TimePrefs, opts: FormatWhenOpts = {}): string {
  const d = isoOrDate instanceof Date ? isoOrDate : new Date(isoOrDate)
  if (Number.isNaN(d.getTime())) return ''
  const timeZone = effectiveTimeZone(prefs)
  const abbr = zoneAbbr(d, timeZone)
  try {
    if (opts.date) {
      const body = d.toLocaleString('en-US', {
        timeZone,
        weekday: 'long',
        month: 'long',
        day: 'numeric',
        hour: 'numeric',
        minute: '2-digit',
        second: opts.seconds ? '2-digit' : undefined,
      })
      return `${body} ${abbr}`
    }
    const body = d.toLocaleTimeString('en-US', {
      timeZone,
      hour: 'numeric',
      minute: '2-digit',
      second: opts.seconds ? '2-digit' : undefined,
    })
    return `${body} ${abbr}`
  } catch {
    return d.toISOString()
  }
}

/** Hour-only label for grids: "7:00 PM EDT". */
export function formatHour(isoOrDate: string | Date | number, prefs: TimePrefs): string {
  return formatWhen(isoOrDate, prefs)
}

/** Range like "7:00 PM – 9:30 PM EDT". */
export function formatTimeRange(
  startISO: string | Date,
  endISO: string | Date,
  prefs: TimePrefs,
  withDate = false,
): string {
  const start = startISO instanceof Date ? startISO : new Date(startISO)
  const end = endISO instanceof Date ? endISO : new Date(endISO)
  if (Number.isNaN(start.getTime()) || Number.isNaN(end.getTime())) return ''
  const timeZone = effectiveTimeZone(prefs)
  const abbr = zoneAbbr(start, timeZone)
  try {
    const startFmt = start.toLocaleString('en-US', {
      timeZone,
      ...(withDate
        ? { weekday: 'long' as const, month: 'long' as const, day: 'numeric' as const }
        : {}),
      hour: 'numeric',
      minute: '2-digit',
    })
    const endFmt = end.toLocaleTimeString('en-US', {
      timeZone,
      hour: 'numeric',
      minute: '2-digit',
    })
    return `${startFmt} – ${endFmt} ${abbr}`
  } catch {
    return `${start.toISOString()} – ${end.toISOString()}`
  }
}
