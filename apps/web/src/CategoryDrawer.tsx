import {
  useCallback,
  useEffect,
  useRef,
  useState,
  type CSSProperties,
  type ReactNode,
  type RefObject,
} from 'react'
import { CategoryPinButton, type CategoryMenuOption } from './CategoryMenu'
import { Icon, icons } from './icons'
import { useIsMobile } from './useMediaQuery'

type SwipeState = {
  startX: number
  startY: number
  fromOpen: boolean
  locked: 'h' | 'v' | null
  width: number
}

export function useCategoryDrawer(resetKey?: string | number) {
  const isMobile = useIsMobile()
  const [open, setOpen] = useState(false)
  const [dragging, setDragging] = useState(false)
  const [shift, setShiftState] = useState<number | null>(null)
  const shiftRef = useRef<number | null>(null)
  const drawerRef = useRef<HTMLDivElement>(null)
  const swipeRef = useRef<SwipeState | null>(null)

  const setShift = useCallback((v: number | null) => {
    shiftRef.current = v
    setShiftState(v)
  }, [])

  const close = useCallback(() => {
    setOpen(false)
    setShift(null)
    setDragging(false)
  }, [setShift])

  const openDrawer = useCallback(() => {
    setOpen(true)
    setShift(null)
    setDragging(false)
  }, [setShift])

  useEffect(() => {
    setOpen(false)
    shiftRef.current = null
    setShiftState(null)
    setDragging(false)
  }, [resetKey])

  useEffect(() => {
    if (!isMobile) {
      setOpen(false)
      shiftRef.current = null
      setShiftState(null)
      setDragging(false)
    }
  }, [isMobile])

  useEffect(() => {
    if (!isMobile || !open) return
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') close()
    }
    const prevOverflow = document.body.style.overflow
    document.body.style.overflow = 'hidden'
    window.addEventListener('keydown', onKey)
    return () => {
      document.body.style.overflow = prevOverflow
      window.removeEventListener('keydown', onKey)
    }
  }, [isMobile, open, close])

  useEffect(() => {
    if (!isMobile) return
    const edge = 28
    const onStart = (e: TouchEvent) => {
      if (e.touches.length !== 1) return
      const t = e.touches[0]
      const width = Math.min(window.innerWidth * 0.86, 320)
      if (open) {
        const drawer = drawerRef.current
        if (!drawer) return
        const rect = drawer.getBoundingClientRect()
        if (t.clientX > rect.right) return
        swipeRef.current = {
          startX: t.clientX,
          startY: t.clientY,
          fromOpen: true,
          locked: null,
          width,
        }
        return
      }
      if (t.clientX <= edge) {
        swipeRef.current = {
          startX: t.clientX,
          startY: t.clientY,
          fromOpen: false,
          locked: null,
          width,
        }
      }
    }
    const onMove = (e: TouchEvent) => {
      const s = swipeRef.current
      if (!s || e.touches.length !== 1) return
      const t = e.touches[0]
      const dx = t.clientX - s.startX
      const dy = t.clientY - s.startY
      if (!s.locked) {
        if (Math.abs(dx) < 8 && Math.abs(dy) < 8) return
        s.locked = Math.abs(dx) >= Math.abs(dy) ? 'h' : 'v'
        if (s.locked === 'v') {
          swipeRef.current = null
          setDragging(false)
          setShift(null)
          return
        }
        setDragging(true)
      }
      if (s.locked !== 'h') return
      e.preventDefault()
      if (s.fromOpen) {
        setShift(Math.min(0, Math.max(-s.width, dx)))
      } else {
        setShift(Math.min(0, Math.max(-s.width, -s.width + Math.max(0, dx))))
      }
    }
    const onEnd = () => {
      const s = swipeRef.current
      swipeRef.current = null
      if (!s) return
      const width = s.width
      const cur = shiftRef.current
      if (s.locked !== 'h' || cur == null) {
        setDragging(false)
        setShift(null)
        return
      }
      const visible = width + cur
      setOpen(visible > width * 0.35)
      requestAnimationFrame(() => {
        setDragging(false)
        setShift(null)
      })
    }
    window.addEventListener('touchstart', onStart, { passive: true })
    window.addEventListener('touchmove', onMove, { passive: false })
    window.addEventListener('touchend', onEnd)
    window.addEventListener('touchcancel', onEnd)
    return () => {
      window.removeEventListener('touchstart', onStart)
      window.removeEventListener('touchmove', onMove)
      window.removeEventListener('touchend', onEnd)
      window.removeEventListener('touchcancel', onEnd)
    }
  }, [isMobile, open, setShift])

  const drawerWidth = typeof window !== 'undefined' ? Math.min(window.innerWidth * 0.86, 320) : 320
  const drawerStyle: CSSProperties | undefined =
    isMobile && shift != null ? { transform: `translate3d(${shift}px, 0, 0)` } : undefined
  const handleStyle: CSSProperties | undefined =
    isMobile && shift != null
      ? { transform: `translate3d(${drawerWidth + shift}px, 0, 0)` }
      : undefined
  const backdropStyle: CSSProperties | undefined =
    isMobile && shift != null
      ? { opacity: Math.min(1, Math.max(0, (drawerWidth + shift) / 280)) }
      : undefined

  return {
    isMobile,
    open,
    dragging,
    drawerRef,
    drawerStyle,
    handleStyle,
    backdropStyle,
    openDrawer,
    close,
    setOpen,
  }
}

/** Mobile-only slide-in category panel (chevron + edge swipe). */
export function CategoryDrawer({
  title,
  open,
  dragging,
  drawerRef,
  drawerStyle,
  handleStyle,
  backdropStyle,
  onOpen,
  onClose,
  children,
}: {
  title: string
  open: boolean
  dragging: boolean
  drawerRef: RefObject<HTMLDivElement | null>
  drawerStyle?: CSSProperties
  handleStyle?: CSSProperties
  backdropStyle?: CSSProperties
  onOpen: () => void
  onClose: () => void
  children: ReactNode
}) {
  return (
    <>
      <button
        type="button"
        className={['cat-drawer-backdrop', open || dragging ? 'is-open' : ''].filter(Boolean).join(' ')}
        style={backdropStyle}
        aria-label="Close categories"
        onClick={onClose}
      />
      <button
        type="button"
        className={['cat-drawer-handle', open ? 'is-open' : ''].filter(Boolean).join(' ')}
        style={handleStyle}
        aria-label={open ? 'Hide categories' : 'Show categories'}
        aria-expanded={open}
        onClick={() => (open ? onClose() : onOpen())}
      >
        <Icon icon={open ? icons.chevronLeft : icons.chevronRight} />
      </button>
      <div
        ref={drawerRef}
        className={['cat-drawer', open ? 'is-open' : ''].filter(Boolean).join(' ')}
        style={drawerStyle}
        aria-hidden={!open && !dragging}
        role="dialog"
        aria-label={title}
      >
        <div className="cat-drawer-head">
          <h2>{title}</h2>
          <button type="button" className="ghost cat-drawer-close" onClick={onClose} aria-label="Close">
            <Icon icon={icons.close} />
          </button>
        </div>
        {children}
      </div>
    </>
  )
}

export function CategoryDrawerChip({ label, onClick }: { label: string; onClick: () => void }) {
  return (
    <button type="button" className="ghost cat-drawer-chip" onClick={onClick}>
      {label}
    </button>
  )
}

/** Shared option list for drawer / sidebar category pickers. */
export function CategoryOptionList({
  value,
  allLabel = 'All categories',
  allCount,
  options,
  pinned,
  onChange,
  onTogglePin,
}: {
  value: string
  allLabel?: string
  allCount?: number | string
  options: CategoryMenuOption[]
  pinned: string[]
  onChange: (value: string) => void
  onTogglePin: (value: string) => void
}) {
  return (
    <>
      <button
        type="button"
        className={!value ? 'vod-cat active' : 'vod-cat'}
        onClick={() => onChange('')}
      >
        <span className="vod-cat-name">{allLabel}</span>
        {allCount != null && allCount !== '' && <span className="muted">{allCount}</span>}
      </button>
      {options.map((opt) => {
        const pinable = opt.pinable !== false
        const isPinned = pinned.includes(opt.value)
        const active = value === opt.value
        return (
          <div key={opt.value} className={active ? 'cat-option active' : 'cat-option'}>
            <button
              type="button"
              className={active ? 'vod-cat active' : 'vod-cat'}
              onClick={() => onChange(opt.value)}
            >
              <span className="vod-cat-name">{opt.label}</span>
              {opt.count != null && opt.count !== '' && <span className="muted">{opt.count}</span>}
            </button>
            {pinable && (
              <CategoryPinButton
                pinned={isPinned}
                label={opt.label}
                onToggle={() => onTogglePin(opt.value)}
              />
            )}
          </div>
        )
      })}
    </>
  )
}
