import { useEffect, useRef, useState } from 'react'
import { api, type MediaFile } from './api'
import { HdrBadges } from './HdrBadges'
import { Icon, icons } from './icons'
import { mediaVersionLabel } from './mediaLabel'
import { usePlayer } from './PlayerContext'

export function MediaActions({
  files,
  title,
  preferredFileId,
}: {
  files: MediaFile[]
  title?: string
  preferredFileId?: string
}) {
  const { play } = usePlayer()
  const [menuOpen, setMenuOpen] = useState(false)
  const [qualityOpen, setQualityOpen] = useState(false)
  const [error, setError] = useState('')
  const rootRef = useRef<HTMLDivElement>(null)

  const list = files.filter((f) => f?.id)
  const preferred = list.find((f) => f.id === preferredFileId) ?? list[0] ?? null

  useEffect(() => {
    if (!menuOpen && !qualityOpen) return
    const onDoc = (e: MouseEvent) => {
      if (!rootRef.current?.contains(e.target as Node)) {
        setMenuOpen(false)
        setQualityOpen(false)
      }
    }
    document.addEventListener('mousedown', onDoc)
    return () => document.removeEventListener('mousedown', onDoc)
  }, [menuOpen, qualityOpen])

  if (!preferred) return null

  const startPlay = (file: MediaFile) => {
    setQualityOpen(false)
    setMenuOpen(false)
    setError('')
    play({ media: file, title })
  }

  const onPlayClick = () => {
    if (list.length > 1) {
      setMenuOpen(false)
      setQualityOpen((v) => !v)
      return
    }
    startPlay(preferred)
  }

  const playExternal = async (file: MediaFile) => {
    setError('')
    setMenuOpen(false)
    setQualityOpen(false)
    try {
      const tok = await api.playbackToken(file.id)
      window.open(tok.external_playlist_url, '_blank', 'noopener,noreferrer')
    } catch (err) {
      setError(err instanceof Error ? err.message : 'External play failed')
    }
  }

  return (
    <div className="media-actions" ref={rootRef}>
      <button type="button" className="play-btn" onClick={onPlayClick} title="Play">
        <Icon icon={icons.play} />
        <span>Play</span>
        {list.length > 1 && <span className="play-count">{list.length}</span>}
      </button>
      <button
        type="button"
        className="menu-btn"
        aria-haspopup="menu"
        aria-expanded={menuOpen}
        title="More"
        onClick={() => {
          setQualityOpen(false)
          setMenuOpen((v) => !v)
        }}
      >
        <Icon icon={icons.menu} />
      </button>

      {qualityOpen && list.length > 1 && (
        <div className="quality-menu" role="menu" aria-label="Choose quality">
          <div className="quality-menu-head">Choose version</div>
          {list.map((f) => (
            <button key={f.id} type="button" role="menuitem" onClick={() => startPlay(f)}>
              <span className="quality-menu-label">
                <span>{mediaVersionLabel(f)}</span>
                <HdrBadges
                  compact
                  flags={{
                    dolby_vision: f.dolby_vision,
                    hdr10: f.hdr10,
                    hdr10_plus: f.hdr10_plus,
                    hlg: f.hlg,
                  }}
                />
              </span>
              <Icon icon={icons.circlePlay} className="quality-play-icon" />
            </button>
          ))}
        </div>
      )}

      {menuOpen && (
        <div className="media-menu" role="menu">
          <button
            type="button"
            role="menuitem"
            onClick={() => {
              if (list.length > 1) {
                setMenuOpen(false)
                setQualityOpen(true)
              } else {
                startPlay(preferred)
              }
            }}
          >
            <Icon icon={icons.circlePlay} /> Play in browser
          </button>
          {list.length === 1 ? (
            <button type="button" role="menuitem" onClick={() => playExternal(preferred)}>
              <Icon icon={icons.external} /> Play in external player
            </button>
          ) : (
            list.map((f) => (
              <button key={f.id} type="button" role="menuitem" onClick={() => playExternal(f)}>
                <Icon icon={icons.external} /> External · {mediaVersionLabel(f)}
              </button>
            ))
          )}
        </div>
      )}

      {error && <div className="error">{error}</div>}
    </div>
  )
}
