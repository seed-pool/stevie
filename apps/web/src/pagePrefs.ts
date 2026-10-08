export const PAGE_SIZE_OPTIONS = [25, 50, 100, 200, 500] as const
export const DEFAULT_PAGE_SIZE = 100

const STORAGE_KEY = 'stevie.browse.pageSize'

export function clampPageSize(n: number): number {
  if (!Number.isFinite(n)) return DEFAULT_PAGE_SIZE
  const v = Math.round(n)
  if ((PAGE_SIZE_OPTIONS as readonly number[]).includes(v)) return v
  // Nearest allowed option.
  let best = DEFAULT_PAGE_SIZE
  let bestDist = Infinity
  for (const opt of PAGE_SIZE_OPTIONS) {
    const d = Math.abs(opt - v)
    if (d < bestDist) {
      best = opt
      bestDist = d
    }
  }
  return best
}

export function getPageSize(): number {
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    if (raw == null || raw === '') return DEFAULT_PAGE_SIZE
    return clampPageSize(Number(raw))
  } catch {
    return DEFAULT_PAGE_SIZE
  }
}

export function setPageSize(n: number) {
  const next = clampPageSize(n)
  try {
    localStorage.setItem(STORAGE_KEY, String(next))
  } catch {
    /* ignore */
  }
  try {
    window.dispatchEvent(new CustomEvent('stevie:page-size', { detail: next }))
  } catch {
    /* ignore */
  }
}
