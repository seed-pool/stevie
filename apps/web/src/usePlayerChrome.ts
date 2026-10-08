import { useCallback, useEffect, useRef, useState } from 'react'

const HIDE_MS = 2600

type Opts = {
  /** Keep chrome visible (e.g. open menus, paused, error). */
  pinned?: boolean
  /** Docked mini-player: still auto-hides, but starts visible. */
  docked?: boolean
}

/**
 * Netflix-style chrome: show on hover / move / tap, hide after idle.
 */
export function usePlayerChrome({ pinned = false, docked = false }: Opts = {}) {
  const [visible, setVisible] = useState(true)
  const timerRef = useRef<number | null>(null)
  const pinnedRef = useRef(pinned)
  pinnedRef.current = pinned

  const clearTimer = useCallback(() => {
    if (timerRef.current != null) {
      window.clearTimeout(timerRef.current)
      timerRef.current = null
    }
  }, [])

  const scheduleHide = useCallback(() => {
    clearTimer()
    if (pinnedRef.current) return
    timerRef.current = window.setTimeout(() => {
      if (!pinnedRef.current) setVisible(false)
    }, HIDE_MS)
  }, [clearTimer])

  const reveal = useCallback(() => {
    setVisible(true)
    scheduleHide()
  }, [scheduleHide])

  const hold = useCallback(() => {
    setVisible(true)
    clearTimer()
  }, [clearTimer])

  useEffect(() => {
    if (pinned) {
      setVisible(true)
      clearTimer()
      return
    }
    scheduleHide()
  }, [pinned, clearTimer, scheduleHide])

  useEffect(() => () => clearTimer(), [clearTimer])

  // Fresh expand/dock: show chrome briefly.
  useEffect(() => {
    setVisible(true)
    scheduleHide()
  }, [docked, scheduleHide])

  return {
    chromeOn: visible || pinned,
    reveal,
    hold,
    scheduleHide,
  }
}
