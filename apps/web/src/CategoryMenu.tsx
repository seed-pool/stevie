import { useEffect, useMemo, useRef, useState } from 'react'
import { Icon, icons } from './icons'

export type CategoryMenuOption = {
  value: string
  label: string
  count?: number | string
  /** When false, no pin control (e.g. Favorites). Default true. */
  pinable?: boolean
}

export function CategoryPinButton({
  pinned,
  onToggle,
  label,
}: {
  pinned: boolean
  onToggle: () => void
  label: string
}) {
  return (
    <button
      type="button"
      className={pinned ? 'cat-pin is-on' : 'cat-pin'}
      title={pinned ? `Remove ${label} from favorites` : `Favorite ${label}`}
      aria-label={pinned ? `Remove ${label} from favorites` : `Favorite ${label}`}
      aria-pressed={pinned}
      onClick={(e) => {
        e.preventDefault()
        e.stopPropagation()
        onToggle()
      }}
    >
      <Icon icon={icons.star} />
    </button>
  )
}

export function CategoryDropdown({
  label = 'Category',
  value,
  allLabel = 'All categories',
  options,
  pinned,
  onChange,
  onTogglePin,
}: {
  label?: string
  value: string
  allLabel?: string
  options: CategoryMenuOption[]
  pinned: string[]
  onChange: (value: string) => void
  onTogglePin: (value: string) => void
}) {
  const [open, setOpen] = useState(false)
  const rootRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (!open) return
    const onDoc = (e: MouseEvent) => {
      if (!rootRef.current?.contains(e.target as Node)) setOpen(false)
    }
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') setOpen(false)
    }
    document.addEventListener('mousedown', onDoc)
    document.addEventListener('keydown', onKey)
    return () => {
      document.removeEventListener('mousedown', onDoc)
      document.removeEventListener('keydown', onKey)
    }
  }, [open])

  const selectedLabel = useMemo(() => {
    if (!value) return allLabel
    return options.find((o) => o.value === value)?.label ?? allLabel
  }, [value, options, allLabel])

  return (
    <div className="cat-dropdown" ref={rootRef}>
      <span className="cat-dropdown-label">{label}</span>
      <button
        type="button"
        className={open ? 'cat-dropdown-trigger is-open' : 'cat-dropdown-trigger'}
        aria-haspopup="listbox"
        aria-expanded={open}
        onClick={() => setOpen((v) => !v)}
      >
        <span className="vod-cat-name">{selectedLabel}</span>
        <span className="cat-dropdown-chevron" aria-hidden>
          ▾
        </span>
      </button>
      {open && (
        <div className="cat-dropdown-menu" role="listbox" aria-label={label}>
          <button
            type="button"
            role="option"
            aria-selected={!value}
            className={!value ? 'vod-cat active' : 'vod-cat'}
            onClick={() => {
              onChange('')
              setOpen(false)
            }}
          >
            <span className="vod-cat-name">{allLabel}</span>
          </button>
          {options.map((opt) => {
            const pinable = opt.pinable !== false
            const isPinned = pinned.includes(opt.value)
            const active = value === opt.value
            return (
              <div key={opt.value} className={active ? 'cat-option active' : 'cat-option'}>
                <button
                  type="button"
                  role="option"
                  aria-selected={active}
                  className={active ? 'vod-cat active' : 'vod-cat'}
                  onClick={() => {
                    onChange(opt.value)
                    setOpen(false)
                  }}
                >
                  <span className="vod-cat-name">{opt.label}</span>
                  {opt.count != null && opt.count !== '' && (
                    <span className="muted">{opt.count}</span>
                  )}
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
        </div>
      )}
    </div>
  )
}
