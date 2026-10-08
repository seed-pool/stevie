/** Letter-leading names first (A…), then numbers / symbols / other. */
export function categoryNameSortKey(name: string): [bucket: number, key: string] {
  const trimmed = (name || '').trim()
  const key = trimmed.toLocaleLowerCase()
  if (!trimmed) return [2, key]
  const ch = trimmed[0]!
  // Unicode letter (includes Latin, Arabic, etc.) — numbers/symbols last.
  if (/^\p{L}/u.test(ch)) return [0, key]
  return [1, key]
}

export function compareCategoryNames(a: string, b: string): number {
  const [ba, ka] = categoryNameSortKey(a)
  const [bb, kb] = categoryNameSortKey(b)
  if (ba !== bb) return ba - bb
  if (ka < kb) return -1
  if (ka > kb) return 1
  if (a < b) return -1
  if (a > b) return 1
  return 0
}

export function sortByCategoryName<T>(items: T[], nameOf: (item: T) => string): T[] {
  return [...items].sort((a, b) => compareCategoryNames(nameOf(a), nameOf(b)))
}
