import { useEffect, useState } from 'react'
import { Link, useSearchParams } from 'react-router-dom'
import {
  api,
  liveLogoUrl,
  posterUrl,
  vodTechBadges,
  type LiveChannel,
  type MediaSearchProgramme,
  type MediaSearchResult,
  type VodMovie,
  type VodSeries,
} from './api'
import { Icon, icons } from './icons'
import { usePlayer } from './PlayerContext'
import { useTimePrefs } from './TimePrefsContext'

function SectionCount({ shown, total }: { shown: number; total: number }) {
  if (!total) return null
  if (shown >= total) return <span className="muted">{total}</span>
  return (
    <span className="muted">
      {shown} of {total}
    </span>
  )
}

function PosterCard({
  to,
  title,
  subtitle,
  poster,
  rating,
  badges,
}: {
  to: string
  title: string
  subtitle?: string
  poster?: string
  rating?: number
  badges?: string
}) {
  return (
    <Link to={to} className="poster-card">
      <div className="poster">
        {poster ? <img src={posterUrl(poster, 'w342')} alt="" loading="lazy" /> : <div className="poster-fallback" />}
        {rating != null && rating > 0 && <span className="rating">{rating.toFixed(1)}</span>}
      </div>
      <div className="poster-meta">
        <strong>{title}</strong>
        <span className="muted">{subtitle}</span>
        {badges && <span className="tech-badge">{badges}</span>}
      </div>
    </Link>
  )
}

export function SearchPage() {
  const [params] = useSearchParams()
  const q = (params.get('q') || '').trim()
  const { playLive } = usePlayer()
  const [data, setData] = useState<MediaSearchResult | null>(null)
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(false)

  useEffect(() => {
    if (!q) {
      setData(null)
      setError('')
      setLoading(false)
      return
    }
    let cancelled = false
    setLoading(true)
    setError('')
    api
      .mediaSearch(q)
      .then((r) => {
        if (!cancelled) setData(r)
      })
      .catch((e: Error) => {
        if (!cancelled) {
          setData(null)
          setError(e.message || 'Search failed')
        }
      })
      .finally(() => {
        if (!cancelled) setLoading(false)
      })
    return () => {
      cancelled = true
    }
  }, [q])

  if (!q) {
    return (
      <div className="search-page">
        <div className="section-head">
          <h1>
            <Icon icon={icons.search} className="section-icon" /> Search
          </h1>
        </div>
        <p className="muted">Type a query in the search bar and press Enter.</p>
      </div>
    )
  }

  const movies = data?.movies ?? []
  const series = data?.series ?? []
  const channels = data?.channels ?? []
  const programmes = data?.programmes ?? []
  const empty =
    !loading &&
    !error &&
    data &&
    !movies.length &&
    !series.length &&
    !channels.length &&
    !programmes.length

  return (
    <div className="search-page">
      <div className="section-head">
        <h1>
          <Icon icon={icons.search} className="section-icon" /> Results for “{q}”
        </h1>
      </div>

      {loading && <p className="muted">Searching…</p>}
      {error && <p className="error">{error}</p>}
      {empty && <p className="muted">No matches across movies, TV shows, or Live TV.</p>}

      {!loading && !error && data && (
        <>
          <SearchMoviesSection movies={movies} total={data.movie_total} />
          <SearchSeriesSection series={series} total={data.series_total} />
          <SearchChannelsSection
            channels={channels}
            total={data.channel_total}
            onPlay={(ch) => playLive(ch.id, ch.name)}
          />
          <SearchProgrammesSection
            programmes={programmes}
            total={data.programme_total}
            onPlay={(p) => playLive(p.channel_id, p.channel_name)}
          />
        </>
      )}
    </div>
  )
}

function SearchMoviesSection({ movies, total }: { movies: VodMovie[]; total: number }) {
  if (!total && !movies.length) return null
  return (
    <section className="search-section">
      <div className="section-head">
        <h2>
          <Icon icon={icons.film} className="section-icon" /> Movies
        </h2>
        <SectionCount shown={movies.length} total={total} />
      </div>
      {movies.length ? (
        <div className="poster-grid">
          {movies.map((m) => (
            <PosterCard
              key={m.id}
              to={`/movies/${m.id}`}
              title={m.name}
              subtitle={[m.year, m.category_name].filter(Boolean).join(' · ')}
              poster={m.poster_url}
              rating={m.rating}
              badges={vodTechBadges(m).slice(0, 3).join(' · ')}
            />
          ))}
        </div>
      ) : (
        <p className="muted">No movies found.</p>
      )}
    </section>
  )
}

function SearchSeriesSection({ series, total }: { series: VodSeries[]; total: number }) {
  if (!total && !series.length) return null
  return (
    <section className="search-section">
      <div className="section-head">
        <h2>
          <Icon icon={icons.tv} className="section-icon" /> TV Shows
        </h2>
        <SectionCount shown={series.length} total={total} />
      </div>
      {series.length ? (
        <div className="poster-grid">
          {series.map((s) => (
            <PosterCard
              key={s.id}
              to={`/shows/${s.id}`}
              title={s.name}
              subtitle={[s.year, s.category_name].filter(Boolean).join(' · ')}
              poster={s.poster_url}
              rating={s.rating}
            />
          ))}
        </div>
      ) : (
        <p className="muted">No TV shows found.</p>
      )}
    </section>
  )
}

function SearchChannelsSection({
  channels,
  total,
  onPlay,
}: {
  channels: LiveChannel[]
  total: number
  onPlay: (ch: LiveChannel) => void
}) {
  if (!total && !channels.length) return null
  return (
    <section className="search-section">
      <div className="section-head">
        <h2>
          <Icon icon={icons.live} className="section-icon" /> Live TV Channels
        </h2>
        <SectionCount shown={channels.length} total={total} />
      </div>
      {channels.length ? (
        <div className="poster-row live-row search-live-grid">
          {channels.map((ch) => {
            const logoSrc = liveLogoUrl(ch.logo_url)
            return (
            <button
              key={ch.id}
              type="button"
              className="live-card"
              onClick={() => onPlay(ch)}
              title={ch.name}
            >
              <div className="live-card-logo">
                {logoSrc ? (
                  <img src={logoSrc} alt="" loading="lazy" />
                ) : (
                  <Icon icon={icons.live} />
                )}
              </div>
              <strong>{ch.name}</strong>
              <span className="muted">{ch.group_title || 'Live'}</span>
            </button>
            )
          })}
        </div>
      ) : (
        <p className="muted">No channels found.</p>
      )}
    </section>
  )
}

function SearchProgrammesSection({
  programmes,
  total,
  onPlay,
}: {
  programmes: MediaSearchProgramme[]
  total: number
  onPlay: (p: MediaSearchProgramme) => void
}) {
  const { formatWhen } = useTimePrefs()
  if (!total && !programmes.length) return null
  return (
    <section className="search-section">
      <div className="section-head">
        <h2>
          <Icon icon={icons.live} className="section-icon" /> Programmes
        </h2>
        <SectionCount shown={programmes.length} total={total} />
      </div>
      {programmes.length ? (
        <div className="search-prog-list">
          {programmes.map((p) => {
            const logoSrc = liveLogoUrl(p.channel_logo)
            return (
            <button
              key={p.id}
              type="button"
              className="search-prog-row"
              onClick={() => onPlay(p)}
            >
              <div className="search-prog-logo">
                {logoSrc ? (
                  <img src={logoSrc} alt="" loading="lazy" />
                ) : (
                  <Icon icon={icons.live} />
                )}
              </div>
              <div className="search-prog-meta">
                <strong>{p.title}</strong>
                <span className="muted">
                  {p.channel_name}
                  {p.group_title ? ` · ${p.group_title}` : ''}
                </span>
                <span className="muted mono">{formatWhen(p.start_time, { date: true })}</span>
                {p.description ? <span className="search-prog-desc muted">{p.description}</span> : null}
              </div>
            </button>
            )
          })}
        </div>
      ) : (
        <p className="muted">No programmes found.</p>
      )}
    </section>
  )
}
