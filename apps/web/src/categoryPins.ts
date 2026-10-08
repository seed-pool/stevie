import { useCallback, useEffect, useState } from 'react'
import { sortByCategoryName } from './categorySort'

export type CategoryPinScope = 'live' | 'movie' | 'series' | 'sports'

const storageKey = (scope: CategoryPinScope) => `stevie.categoryPins.${scope}`

function readPins(scope: CategoryPinScope): string[] {
  try {
    const raw = localStorage.getItem(storageKey(scope))
    if (!raw) return []
    const parsed = JSON.parse(raw)
    if (!Array.isArray(parsed)) return []
    return parsed.filter((x): x is string => typeof x === 'string' && x !== '')
  } catch {
    return []
  }
}

function writePins(scope: CategoryPinScope, ids: string[]) {
  try {
    localStorage.setItem(storageKey(scope), JSON.stringify(ids))
  } catch {
    /* ignore */
  }
  try {
    window.dispatchEvent(new CustomEvent('stevie:category-pins', { detail: { scope, ids } }))
  } catch {
    /* ignore */
  }
}

export function getPinnedCategories(scope: CategoryPinScope): string[] {
  return readPins(scope)
}

/** Newest pin first. */
export function togglePinnedCategory(scope: CategoryPinScope, id: string): string[] {
  const key = id.trim()
  if (!key) return readPins(scope)
  const cur = readPins(scope)
  const next = cur.includes(key) ? cur.filter((x) => x !== key) : [key, ...cur.filter((x) => x !== key)]
  writePins(scope, next)
  return next
}

export function useCategoryPins(scope: CategoryPinScope) {
  const [pinned, setPinned] = useState(() => getPinnedCategories(scope))

  useEffect(() => {
    setPinned(getPinnedCategories(scope))
  }, [scope])

  useEffect(() => {
    const onChange = (e: Event) => {
      const detail = (e as CustomEvent<{ scope?: CategoryPinScope; ids?: string[] }>).detail
      if (detail?.scope && detail.scope !== scope) return
      setPinned(Array.isArray(detail?.ids) ? detail.ids : getPinnedCategories(scope))
    }
    window.addEventListener('stevie:category-pins', onChange)
    return () => window.removeEventListener('stevie:category-pins', onChange)
  }, [scope])

  const toggle = useCallback(
    (id: string) => {
      setPinned(togglePinnedCategory(scope, id))
    },
    [scope],
  )

  return { pinned, toggle, isPinned: (id: string) => pinned.includes(id) }
}

/** Pinned ids first (in pin order), then remaining A→Z by name. */
export function sortCategoriesWithPins<T>(
  items: T[],
  pinned: string[],
  idOf: (item: T) => string,
  nameOf: (item: T) => string,
): T[] {
  const byId = new Map(items.map((item) => [idOf(item), item]))
  const pinnedItems: T[] = []
  const seen = new Set<string>()
  for (const id of pinned) {
    const item = byId.get(id)
    if (!item || seen.has(id)) continue
    pinnedItems.push(item)
    seen.add(id)
  }
  const rest = sortByCategoryName(
    items.filter((item) => !seen.has(idOf(item))),
    nameOf,
  )
  return [...pinnedItems, ...rest]
}
