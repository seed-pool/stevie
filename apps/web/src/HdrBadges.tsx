export type HdrFlags = {
  dolby_vision?: boolean
  hdr10?: boolean
  hdr10_plus?: boolean
  hlg?: boolean
}

export function HdrBadges({ flags, compact = false }: { flags: HdrFlags; compact?: boolean }) {
  return (
    <span className={compact ? 'hdr-badges compact' : 'hdr-badges'}>
      {flags.dolby_vision && (
        <span className="hdr-badge dv" title="Dolby Vision" aria-label="Dolby Vision">
          <svg viewBox="0 0 72 16" role="img" focusable="false">
            <rect width="72" height="16" rx="3" fill="#0b0b0f" />
            <text x="36" y="11.5" textAnchor="middle" fill="#c9b7ff" fontSize="7.2" fontFamily="Arial, Helvetica, sans-serif" fontWeight="700" letterSpacing="0.4">
              DOLBY VISION
            </text>
          </svg>
        </span>
      )}
      {flags.hdr10_plus && (
        <span className="hdr-badge hdr10p" title="HDR10+" aria-label="HDR10+">
          <svg viewBox="0 0 48 16" role="img" focusable="false">
            <rect width="48" height="16" rx="3" fill="#111827" />
            <text x="24" y="11.5" textAnchor="middle" fill="#7dd3fc" fontSize="8" fontFamily="Arial, Helvetica, sans-serif" fontWeight="800" letterSpacing="0.3">
              HDR10+
            </text>
          </svg>
        </span>
      )}
      {flags.hdr10 && !flags.hdr10_plus && (
        <span className="hdr-badge hdr10" title="HDR10" aria-label="HDR10">
          <svg viewBox="0 0 40 16" role="img" focusable="false">
            <defs>
              <linearGradient id="hdr10grad" x1="0" y1="0" x2="1" y2="1">
                <stop offset="0%" stopColor="#38bdf8" />
                <stop offset="100%" stopColor="#a78bfa" />
              </linearGradient>
            </defs>
            <rect width="40" height="16" rx="3" fill="#0f172a" stroke="url(#hdr10grad)" strokeWidth="1.2" />
            <text x="20" y="11.5" textAnchor="middle" fill="#e2e8f0" fontSize="8" fontFamily="Arial, Helvetica, sans-serif" fontWeight="800" letterSpacing="0.4">
              HDR10
            </text>
          </svg>
        </span>
      )}
      {flags.hlg && (
        <span className="hdr-badge hlg" title="HLG" aria-label="HLG">
          <svg viewBox="0 0 32 16" role="img" focusable="false">
            <rect width="32" height="16" rx="3" fill="#14532d" />
            <text x="16" y="11.5" textAnchor="middle" fill="#bbf7d0" fontSize="8" fontFamily="Arial, Helvetica, sans-serif" fontWeight="800">
              HLG
            </text>
          </svg>
        </span>
      )}
    </span>
  )
}
