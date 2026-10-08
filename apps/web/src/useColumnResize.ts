import { useCallback, useRef, type PointerEvent as ReactPointerEvent } from 'react'

/** Horizontal drag-resize with optional localStorage persistence. */
export function useColumnResize(
  value: number,
  setValue: (n: number) => void,
  min: number,
  max: number,
  storageKey?: string,
) {
  const dragRef = useRef<{ startX: number; startW: number } | null>(null)

  const onPointerDown = useCallback(
    (e: ReactPointerEvent<HTMLDivElement>) => {
      e.preventDefault()
      e.stopPropagation()
      e.currentTarget.setPointerCapture(e.pointerId)
      dragRef.current = { startX: e.clientX, startW: value }
      document.body.classList.add('col-resizing', 'epg-resizing')
    },
    [value],
  )

  const onPointerMove = useCallback(
    (e: ReactPointerEvent<HTMLDivElement>) => {
      if (!dragRef.current) return
      const next = Math.round(
        Math.min(max, Math.max(min, dragRef.current.startW + (e.clientX - dragRef.current.startX))),
      )
      setValue(next)
    },
    [max, min, setValue],
  )

  const onPointerUp = useCallback(
    (e: ReactPointerEvent<HTMLDivElement>) => {
      if (!dragRef.current) return
      dragRef.current = null
      document.body.classList.remove('col-resizing', 'epg-resizing')
      if (storageKey) {
        try {
          localStorage.setItem(storageKey, String(value))
        } catch {
          /* ignore */
        }
      }
      try {
        e.currentTarget.releasePointerCapture(e.pointerId)
      } catch {
        /* ignore */
      }
    },
    [storageKey, value],
  )

  return { onPointerDown, onPointerMove, onPointerUp }
}

export function loadStoredWidth(key: string, fallback: number, min: number, max: number) {
  try {
    const v = Number(localStorage.getItem(key))
    if (!Number.isFinite(v) || v <= 0) return fallback
    return Math.min(max, Math.max(min, Math.round(v)))
  } catch {
    return fallback
  }
}
