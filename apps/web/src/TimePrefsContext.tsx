import { createContext, useContext, useMemo, useState, useCallback, type ReactNode } from 'react'
import {
  formatHour,
  formatTimeRange,
  formatWhen,
  getTimePrefs,
  setTimeZone as persistTimeZone,
  setUseLocalTime as persistUseLocalTime,
  type FormatWhenOpts,
  type TimePrefs,
} from './timePrefs'

type TimePrefsCtx = {
  prefs: TimePrefs
  setUseLocalTime: (on: boolean) => void
  setTimeZone: (tz: string) => void
  formatWhen: (iso: string | Date | number, opts?: FormatWhenOpts) => string
  formatHour: (iso: string | Date | number) => string
  formatTimeRange: (start: string | Date, end: string | Date, withDate?: boolean) => string
}

const Ctx = createContext<TimePrefsCtx | null>(null)

export function TimePrefsProvider({ children }: { children: ReactNode }) {
  const [prefs, setPrefs] = useState<TimePrefs>(() => getTimePrefs())

  const setUseLocalTime = useCallback((on: boolean) => {
    persistUseLocalTime(on)
    setPrefs((p) => ({ ...p, useLocalTime: on }))
  }, [])

  const setTimeZone = useCallback((tz: string) => {
    persistTimeZone(tz)
    setPrefs((p) => ({ ...p, timeZone: tz }))
  }, [])

  const value = useMemo<TimePrefsCtx>(
    () => ({
      prefs,
      setUseLocalTime,
      setTimeZone,
      formatWhen: (iso, opts) => formatWhen(iso, prefs, opts),
      formatHour: (iso) => formatHour(iso, prefs),
      formatTimeRange: (start, end, withDate) => formatTimeRange(start, end, prefs, withDate),
    }),
    [prefs, setUseLocalTime, setTimeZone],
  )

  return <Ctx.Provider value={value}>{children}</Ctx.Provider>
}

export function useTimePrefs() {
  const ctx = useContext(Ctx)
  if (!ctx) {
    // Fallback for tests / rare mounts outside provider — UTC defaults.
    const prefs = getTimePrefs()
    return {
      prefs,
      setUseLocalTime: () => {},
      setTimeZone: () => {},
      formatWhen: (iso: string | Date | number, opts?: FormatWhenOpts) => formatWhen(iso, prefs, opts),
      formatHour: (iso: string | Date | number) => formatHour(iso, prefs),
      formatTimeRange: (start: string | Date, end: string | Date, withDate?: boolean) =>
        formatTimeRange(start, end, prefs, withDate),
    } satisfies TimePrefsCtx
  }
  return ctx
}
