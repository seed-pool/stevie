export function PaginationBar({
  page,
  pageSize,
  total,
  onPageChange,
  disabled,
}: {
  page: number
  pageSize: number
  total: number
  onPageChange: (page: number) => void
  disabled?: boolean
}) {
  const pages = Math.max(1, Math.ceil(Math.max(0, total) / Math.max(1, pageSize)))
  const current = Math.min(Math.max(1, page), pages)
  const from = total === 0 ? 0 : (current - 1) * pageSize + 1
  const to = Math.min(total, current * pageSize)

  if (total <= pageSize && current <= 1) {
    return (
      <div className="pagination muted">
        {total} item{total === 1 ? '' : 's'}
      </div>
    )
  }

  return (
    <div className="pagination">
      <span className="muted">
        {from}–{to} of {total}
      </span>
      <div className="pagination-controls">
        <button
          type="button"
          className="ghost"
          disabled={disabled || current <= 1}
          onClick={() => onPageChange(1)}
          title="First page"
        >
          First
        </button>
        <button
          type="button"
          className="ghost"
          disabled={disabled || current <= 1}
          onClick={() => onPageChange(current - 1)}
          title="Previous page"
        >
          Prev
        </button>
        <span className="pagination-page mono">
          {current} / {pages}
        </span>
        <button
          type="button"
          className="ghost"
          disabled={disabled || current >= pages}
          onClick={() => onPageChange(current + 1)}
          title="Next page"
        >
          Next
        </button>
        <button
          type="button"
          className="ghost"
          disabled={disabled || current >= pages}
          onClick={() => onPageChange(pages)}
          title="Last page"
        >
          Last
        </button>
      </div>
    </div>
  )
}
