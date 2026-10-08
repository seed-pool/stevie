import { useCallback, useEffect, useMemo, useRef, useState, type CSSProperties } from 'react'
import { Link, NavLink, Navigate, Route, Routes, useLocation, useNavigate, useParams } from 'react-router-dom'
import {
  api,
  formatBytes,
  formatDuration,
  formatEta,
  formatSpeed,
  liveLogoUrl,
  notifyLiveRecordingChange,
  parseDurationSec,
  posterUrl,
  vodTechBadges,
  type LiveChannel,
  type LiveRecordingJob,
  type LiveSettings,
  type RecordingFile,
  type ScheduledRecording,
  type MediaFile,
  type Status,
  type User,
  type VodCategory,
  type VodAnalyzeResult,
  type VodDownload,
  type VodEpisode,
  type VodMovie,
  type VodSeries,
  type VodSyncProgress,
  type EPGSyncProgress,
  type XtreamCategory,
  type XtreamSyncProgress,
} from './api'
import { CategoryPinButton } from './CategoryMenu'
import { sortByCategoryName } from './categorySort'
import { sortCategoriesWithPins, useCategoryPins } from './categoryPins'
import { loadStoredWidth, useColumnResize } from './useColumnResize'
import { HdrBadges } from './HdrBadges'
import { Icon, icons } from './icons'
import { LangFlag } from './lang'
import { LiveGuide } from './LiveGuide'
import { SportsPage } from './SportsPage'
import { MediaActions } from './MediaActions'
import { mediaVersionLabel } from './mediaLabel'
import { PlayerProvider, usePlayer } from './PlayerContext'
import { SearchPage } from './SearchPage'
import { PaginationBar } from './Pagination'
import { DEFAULT_PAGE_SIZE, getPageSize, PAGE_SIZE_OPTIONS, setPageSize } from './pagePrefs'
import { getShowPlayerTechInfo, setShowPlayerTechInfo } from './playerPrefs'
import { applyTheme, getStoredTheme, setTheme, type Theme } from './theme'
import { TimePrefsProvider, useTimePrefs } from './TimePrefsContext'
import {
  TIMEZONE_OPTIONS,
  detectBrowserTimeZone,
  effectiveTimeZone,
} from './timePrefs'
import { useIsMobile } from './useMediaQuery'
import './App.css'

export default function App() {
  const [user, setUser] = useState<User | null>(null)
  const [loading, setLoading] = useState(true)
  const [theme, setThemeState] = useState<Theme>(() => getStoredTheme())

  useEffect(() => {
    applyTheme(theme)
  }, [theme])

  useEffect(() => {
    api
      .me()
      .then((r) => setUser(r.user))
      .catch(() => setUser(null))
      .finally(() => setLoading(false))
  }, [])

  const changeTheme = (next: Theme) => {
    setTheme(next)
    setThemeState(next)
  }

  if (loading) {
    return <div className="boot mono">boot://stevie …</div>
  }

  if (!user) {
    return <Login onLogin={setUser} />
  }

  return (
    <PlayerProvider>
      <TimePrefsProvider>
        <div className="app">
          <Header
            user={user}
            onLogout={async () => {
              await api.logout()
              setUser(null)
            }}
          />
          <main className="main">
            <Routes>
              <Route path="/" element={<Home />} />
              <Route path="/movies" element={<MoviesPage />} />
              <Route path="/movies/:id" element={<MovieDetail />} />
              <Route path="/shows" element={<ShowsPage />} />
              <Route path="/shows/:id" element={<ShowDetail />} />
              <Route path="/live" element={<LiveGuide />} />
              <Route path="/sports" element={<SportsPage />} />
              <Route path="/search" element={<SearchPage />} />
              <Route path="/recordings" element={<RecordingsPage />} />
              <Route path="/settings" element={<Settings theme={theme} onThemeChange={changeTheme} />} />
              <Route path="*" element={<Navigate to="/" replace />} />
            </Routes>
          </main>
        </div>
      </TimePrefsProvider>
    </PlayerProvider>
  )
}

function downloadProgressLabel(d: VodDownload) {
  const parts: string[] = []
  if (d.status === 'finalizing') parts.push('Finalizing')
  else parts.push('Downloading')
  if (d.total_bytes && d.total_bytes > 0) {
    parts.push(`${formatBytes(d.bytes ?? 0)} / ${formatBytes(d.total_bytes)}`)
  } else if (d.bytes) {
    parts.push(formatBytes(d.bytes))
  }
  const speed = formatSpeed(d.bytes_per_sec)
  if (speed) parts.push(speed)
  const eta = formatEta(d.eta_sec)
  if (eta) parts.push(`ETA ${eta}`)
  return parts.join(' · ')
}

function RecordingBanner() {
  const navigate = useNavigate()
  const { formatWhen } = useTimePrefs()
  const [jobs, setJobs] = useState<LiveRecordingJob[]>([])
  const [downloads, setDownloads] = useState<VodDownload[]>([])
  const [nextScheduled, setNextScheduled] = useState<ScheduledRecording | null>(null)
  const [scheduledCount, setScheduledCount] = useState(0)
  const [stopping, setStopping] = useState<string | null>(null)
  const [cancelling, setCancelling] = useState<string | null>(null)

  const refresh = useCallback(() => {
    api
      .liveRecordings()
      .then((r) => {
        setJobs(r.active ?? r.recordings ?? [])
        setDownloads(
          (r.downloads ?? []).filter((d) => d.status === 'downloading' || d.status === 'finalizing'),
        )
        const upcoming = (r.scheduled ?? [])
          .filter((s) => s.status === 'scheduled' || s.status === 'starting')
          .slice()
          .sort((a, b) => new Date(a.start_time).getTime() - new Date(b.start_time).getTime())
        setScheduledCount(upcoming.length)
        setNextScheduled(upcoming[0] ?? null)
      })
      .catch(() => {
        setJobs([])
        setDownloads([])
        setNextScheduled(null)
        setScheduledCount(0)
      })
  }, [])

  useEffect(() => {
    refresh()
    const t = window.setInterval(refresh, 2000)
    const onChange = () => refresh()
    window.addEventListener('stevie:live-recordings', onChange)
    return () => {
      window.clearInterval(t)
      window.removeEventListener('stevie:live-recordings', onChange)
    }
  }, [refresh])

  const goRecordings = () => navigate('/recordings')

  if (!jobs.length && !nextScheduled && !downloads.length) return null

  return (
    <>
      {downloads.length > 0 && (
        <div
          className="recording-banner is-download is-clickable"
          role="link"
          tabIndex={0}
          onClick={goRecordings}
          onKeyDown={(e) => {
            if (e.key === 'Enter' || e.key === ' ') {
              e.preventDefault()
              goRecordings()
            }
          }}
        >
          {downloads.map((d) => (
            <div key={d.id} className="recording-banner-item">
              <span className="recording-dot is-download" aria-hidden />
              <span className="recording-banner-label">
                DL · <strong>{d.title}</strong>
                <span className="muted"> · {downloadProgressLabel(d)}</span>
              </span>
              <button
                type="button"
                className="ghost recording-stop-btn"
                disabled={cancelling === d.id}
                onClick={async (e) => {
                  e.stopPropagation()
                  setCancelling(d.id)
                  try {
                    await api.cancelVodDownload(d.id)
                    notifyLiveRecordingChange()
                    refresh()
                  } catch {
                    refresh()
                  } finally {
                    setCancelling(null)
                  }
                }}
              >
                {cancelling === d.id ? 'Cancelling…' : 'Cancel'}
              </button>
            </div>
          ))}
        </div>
      )}
      {nextScheduled && (
        <div
          className="recording-banner is-scheduled is-clickable"
          role="link"
          tabIndex={0}
          onClick={goRecordings}
          onKeyDown={(e) => {
            if (e.key === 'Enter' || e.key === ' ') {
              e.preventDefault()
              goRecordings()
            }
          }}
        >
          <div className="recording-banner-item">
            <span className="recording-dot is-scheduled" aria-hidden />
            <span className="recording-banner-label">
              SCHEDULED · <strong>{nextScheduled.channel_name}</strong>
              {nextScheduled.title ? <span className="muted"> · {nextScheduled.title}</span> : null}
              <span className="muted">
                {' '}
                · {formatWhen(nextScheduled.start_time, { date: true })}
              </span>
              {scheduledCount > 1 ? (
                <span className="muted"> · +{scheduledCount - 1} more</span>
              ) : null}
            </span>
            <button
              type="button"
              className="ghost recording-stop-btn"
              disabled={cancelling === nextScheduled.id}
              onClick={async (e) => {
                e.stopPropagation()
                setCancelling(nextScheduled.id)
                try {
                  await api.cancelScheduledRecording(nextScheduled.id)
                  notifyLiveRecordingChange()
                  refresh()
                } catch {
                  refresh()
                } finally {
                  setCancelling(null)
                }
              }}
            >
              {cancelling === nextScheduled.id ? 'Cancelling…' : 'Cancel'}
            </button>
          </div>
        </div>
      )}
      {jobs.length > 0 && (
        <div
          className="recording-banner is-clickable"
          role="link"
          tabIndex={0}
          onClick={goRecordings}
          onKeyDown={(e) => {
            if (e.key === 'Enter' || e.key === ' ') {
              e.preventDefault()
              goRecordings()
            }
          }}
        >
          {jobs.map((job) => (
            <div key={job.channel_id} className="recording-banner-item">
              <span className="recording-dot" aria-hidden />
              <span className="recording-banner-label">
                REC · <strong>{job.channel_name}</strong>
                {job.program_title ? <span className="muted"> · {job.program_title}</span> : null}
              </span>
              <button
                type="button"
                className="ghost recording-stop-btn"
                disabled={stopping === job.channel_id || job.status === 'stopping'}
                onClick={async (e) => {
                  e.stopPropagation()
                  setStopping(job.channel_id)
                  try {
                    await api.stopLiveRecording(job.channel_id)
                    notifyLiveRecordingChange()
                    refresh()
                  } catch {
                    refresh()
                  } finally {
                    setStopping(null)
                  }
                }}
              >
                {stopping === job.channel_id || job.status === 'stopping' ? 'Stopping…' : 'Stop'}
              </button>
            </div>
          ))}
        </div>
      )}
    </>
  )
}

function Header({ user, onLogout }: { user: User; onLogout: () => void }) {
  const navigate = useNavigate()
  const location = useLocation()
  const isMobile = useIsMobile()
  const [q, setQ] = useState(() => {
    if (location.pathname !== '/search') return ''
    return new URLSearchParams(location.search).get('q') || ''
  })

  useEffect(() => {
    if (location.pathname !== '/search') return
    setQ(new URLSearchParams(location.search).get('q') || '')
  }, [location.pathname, location.search])

  const submitSearch = () => {
    const query = q.trim()
    if (!query) return
    navigate(`/search?q=${encodeURIComponent(query)}`)
  }

  const tabs = (
    <>
      <NavLink to="/" end className={({ isActive }) => (isActive ? 'tab active' : 'tab')}>
        <Icon icon={icons.home} />
        <span>Home</span>
      </NavLink>
      <NavLink to="/movies" className={({ isActive }) => (isActive ? 'tab active' : 'tab')}>
        <Icon icon={icons.film} />
        <span>Movies</span>
      </NavLink>
      <NavLink to="/shows" className={({ isActive }) => (isActive ? 'tab active' : 'tab')}>
        <Icon icon={icons.tv} />
        <span>{isMobile ? 'TV' : 'TV Shows'}</span>
      </NavLink>
      <NavLink to="/live" className={({ isActive }) => (isActive ? 'tab active' : 'tab')}>
        <Icon icon={icons.live} />
        <span>{isMobile ? 'Live' : 'Live TV'}</span>
      </NavLink>
      <NavLink to="/sports" className={({ isActive }) => (isActive ? 'tab active' : 'tab')}>
        <Icon icon={icons.sports} />
        <span>Sports</span>
      </NavLink>
      <NavLink to="/recordings" className={({ isActive }) => (isActive ? 'tab active' : 'tab')}>
        <Icon icon={icons.recordings} />
        <span>{isMobile ? 'Recs' : 'Recordings'}</span>
      </NavLink>
    </>
  )

  return (
    <div className="app-chrome">
      <RecordingBanner />
      <header className="topbar">
        <Link to="/" className="brand">
          Stevie
        </Link>
        <nav className="primary-tabs desktop-only" aria-label="Library sections">
          {tabs}
        </nav>
        <form
          className="search"
          role="search"
          onSubmit={(e) => {
            e.preventDefault()
            submitSearch()
          }}
        >
          <button type="submit" className="search-icon-btn" aria-label="Search" title="Search">
            <Icon icon={icons.search} />
          </button>
          <input
            value={q}
            onChange={(e) => setQ(e.target.value)}
            placeholder={isMobile ? 'Search…' : 'Search movies, TV & Live'}
            aria-label="Search movies, TV and Live"
            enterKeyHint="search"
          />
        </form>
        <nav className="nav">
          <Link to="/settings" className="nav-link" title="Settings" aria-label="Settings">
            <Icon icon={icons.settings} />
            <span className="nav-link-label">Settings</span>
          </Link>
          <span className="muted nav-user">{user.username}</span>
          <button
            type="button"
            className="ghost nav-link"
            onClick={onLogout}
            title="Sign out"
            aria-label="Sign out"
          >
            <Icon icon={icons.logout} />
            <span className="nav-link-label">Sign out</span>
          </button>
        </nav>
      </header>
      <nav className="bottom-tabs mobile-only" aria-label="Library sections">
        {tabs}
      </nav>
    </div>
  )
}

function Login({ onLogin }: { onLogin: (u: User) => void }) {
  const [username, setUsername] = useState('admin')
  const [password, setPassword] = useState('')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)

  return (
    <div className="login-shell">
      <form
        className="login-card"
        onSubmit={async (e) => {
          e.preventDefault()
          setBusy(true)
          setError('')
          try {
            const r = await api.login(username, password)
            onLogin(r.user)
          } catch (err) {
            setError(err instanceof Error ? err.message : 'Login failed')
          } finally {
            setBusy(false)
          }
        }}
      >
        <div className="brand large">Stevie</div>
        <p className="muted">self-hosted media console</p>
        <label>
          Username
          <input value={username} onChange={(e) => setUsername(e.target.value)} autoComplete="username" />
        </label>
        <label>
          Password
          <input
            type="password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            autoComplete="current-password"
          />
        </label>
        {error && <div className="error">{error}</div>}
        <button type="submit" disabled={busy}>
          {busy ? 'Signing in…' : 'Sign in'}
        </button>
      </form>
    </div>
  )
}

function RecordingLogo({ url, label }: { url?: string; label: string }) {
  const src = liveLogoUrl(url)
  return (
    <div className="recording-logo" aria-hidden={!src}>
      {src ? (
        <img
          src={src}
          alt=""
          loading="lazy"
          onError={(e) => {
            e.currentTarget.style.display = 'none'
            const fallback = e.currentTarget.nextElementSibling
            if (fallback instanceof HTMLElement) fallback.hidden = false
          }}
        />
      ) : null}
      <span className="recording-logo-fallback" hidden={!!src} title={label}>
        <Icon icon={icons.live} />
      </span>
    </div>
  )
}

function formatSlotRange(startISO: string, endISO: string, formatRange: (a: string, b: string, withDate?: boolean) => string) {
  return formatRange(startISO, endISO, true)
}

function RecordingsPage() {
  const { playRecording } = usePlayer()
  const { formatWhen, formatTimeRange } = useTimePrefs()
  const [files, setFiles] = useState<RecordingFile[]>([])
  const [scheduled, setScheduled] = useState<ScheduledRecording[]>([])
  const [active, setActive] = useState<LiveRecordingJob[]>([])
  const [downloads, setDownloads] = useState<VodDownload[]>([])
  const [error, setError] = useState('')
  const [busy, setBusy] = useState<string | null>(null)

  const refresh = useCallback(() => {
    api
      .liveRecordings()
      .then((r) => {
        setFiles(r.files ?? [])
        setScheduled(r.scheduled ?? [])
        setActive(r.active ?? r.recordings ?? [])
        setDownloads(r.downloads ?? [])
      })
      .catch((err) => setError(err instanceof Error ? err.message : 'Failed to load recordings'))
  }, [])

  const activeDownloads = downloads.filter(
    (d) => d.status === 'downloading' || d.status === 'finalizing',
  )

  useEffect(() => {
    refresh()
    const onChange = () => refresh()
    window.addEventListener('stevie:live-recordings', onChange)
    const t = window.setInterval(refresh, activeDownloads.length ? 2000 : 8000)
    return () => {
      window.removeEventListener('stevie:live-recordings', onChange)
      window.clearInterval(t)
    }
  }, [refresh, activeDownloads.length])

  const pending = scheduled.filter(
    (s) => s.status === 'scheduled' || s.status === 'starting' || s.status === 'recording',
  )
  const activeByFile = useMemo(() => {
    const m = new Map<string, LiveRecordingJob>()
    for (const job of active) {
      if (job.file_name) m.set(job.file_name, job)
    }
    return m
  }, [active])
  const empty = !files.length && !pending.length && !active.length && !activeDownloads.length && !error

  const cancelSchedule = async (id: string) => {
    setBusy(id)
    setError('')
    try {
      await api.cancelScheduledRecording(id)
      notifyLiveRecordingChange()
      refresh()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Cancel failed')
    } finally {
      setBusy(null)
    }
  }

  const stopChannel = async (channelId: string, busyKey: string) => {
    setBusy(busyKey)
    setError('')
    try {
      await api.stopLiveRecording(channelId)
      notifyLiveRecordingChange()
      refresh()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Stop failed')
    } finally {
      setBusy(null)
    }
  }

  return (
    <div className="recordings-page">
      <div className="section-head">
        <h1>
          <Icon icon={icons.recordings} className="section-icon" /> Recordings
        </h1>
        <span className="muted">
          {active.length ? `${active.length} recording · ` : ''}
          {activeDownloads.length ? `${activeDownloads.length} downloading · ` : ''}
          {pending.length ? `${pending.length} scheduled · ` : ''}
          {files.length} file{files.length === 1 ? '' : 's'}
        </span>
      </div>
      {error && <div className="error">{error}</div>}
      {empty && (
        <div className="status-card" style={{ padding: '1.2rem' }}>
          <p>No recordings yet.</p>
          <p className="muted">
            Record from Live TV, schedule from the guide, or download a movie/episode from Movies / TV
            Shows.
          </p>
          <Link to="/live">Open Live TV</Link>
        </div>
      )}
      {activeDownloads.length > 0 && (
        <section className="recordings-section is-scheduled">
          <div className="recordings-section-head">
            <h2>Downloading</h2>
            <span className="muted">{activeDownloads.length}</span>
          </div>
          <div className="recording-list">
            {activeDownloads.map((d) => (
              <div key={d.id} className="recording-row is-live-rec">
                <div className="recording-logo" aria-hidden>
                  <Icon icon={icons.download} />
                </div>
                <div className="recording-row-main">
                  <strong>{d.title}</strong>
                  <span className="recording-meta">
                    <span className="muted">
                      {downloadProgressLabel(d)}
                      {d.kind === 'episode' ? ' · Episode' : ' · Movie'}
                    </span>
                  </span>
                  {d.total_bytes && d.total_bytes > 0 ? (
                    <div
                      className="download-progress"
                      role="progressbar"
                      aria-valuemin={0}
                      aria-valuemax={100}
                      aria-valuenow={Math.min(100, Math.round(((d.bytes ?? 0) / d.total_bytes) * 100))}
                    >
                      <div
                        className="download-progress-bar"
                        style={{ width: `${Math.min(100, ((d.bytes ?? 0) / d.total_bytes) * 100)}%` }}
                      />
                    </div>
                  ) : null}
                </div>
                <div className="recording-row-actions">
                  <button
                    type="button"
                    className="ghost"
                    disabled={busy === d.id}
                    onClick={async () => {
                      setBusy(d.id)
                      try {
                        await api.cancelVodDownload(d.id)
                        notifyLiveRecordingChange()
                        refresh()
                      } catch (err) {
                        setError(err instanceof Error ? err.message : 'Cancel failed')
                      } finally {
                        setBusy(null)
                      }
                    }}
                  >
                    Cancel
                  </button>
                </div>
              </div>
            ))}
          </div>
        </section>
      )}
      {pending.length > 0 && (
        <section className="recordings-section is-scheduled">
          <div className="recordings-section-head">
            <h2>Scheduled</h2>
            <span className="muted">{pending.length}</span>
          </div>
          <div className="recording-list">
            {pending.map((s) => (
              <div
                key={s.id}
                className={[
                  'recording-row',
                  s.status === 'recording' ? 'is-live-rec' : 'is-scheduled-rec',
                ].join(' ')}
              >
                <RecordingLogo url={s.logo_url} label={s.channel_name} />
                <div className="recording-row-main">
                  <strong>{s.title || s.channel_name}</strong>
                  <span className="recording-meta">
                    <span className="recording-channel">{s.channel_name}</span>
                    <span className="muted">
                      {s.status === 'recording'
                        ? 'Recording now'
                        : s.status === 'starting'
                          ? 'Starting'
                          : 'Scheduled'}
                      {' · '}
                      {formatSlotRange(s.start_time, s.end_time, formatTimeRange)}
                      {s.category ? ` · ${s.category}` : ''}
                    </span>
                  </span>
                  {s.description ? <p className="recording-epg-desc muted">{s.description}</p> : null}
                </div>
                <div className="recording-row-actions">
                  {(s.status === 'scheduled' || s.status === 'starting') && (
                    <button
                      type="button"
                      className="ghost"
                      disabled={busy === s.id}
                      onClick={() => void cancelSchedule(s.id)}
                    >
                      Cancel
                    </button>
                  )}
                  {s.status === 'recording' && (
                    <button
                      type="button"
                      className="ghost"
                      disabled={busy === s.id || busy === s.channel_id}
                      onClick={() => void cancelSchedule(s.id)}
                    >
                      {busy === s.id ? 'Stopping…' : 'Stop'}
                    </button>
                  )}
                </div>
              </div>
            ))}
          </div>
        </section>
      )}
      {(files.length > 0 || pending.length > 0 || active.length > 0) && (
        <section className="recordings-section is-library">
          <div className="recordings-section-head">
            <h2>Library</h2>
            <span className="muted">
              {files.length} file{files.length === 1 ? '' : 's'}
            </span>
          </div>
          {!files.length ? (
            <p className="muted recordings-empty-note">No saved recordings yet.</p>
          ) : (
            <div className="recording-list">
              {files.map((f) => {
                const liveJob = activeByFile.get(f.name)
                return (
                  <div
                    key={f.name}
                    className={['recording-row', f.recording ? 'is-live-rec' : ''].filter(Boolean).join(' ')}
                  >
                    <RecordingLogo url={f.logo_url} label={f.channel_name || f.title || f.name} />
                    <div className="recording-row-main">
                      <strong>{f.program_title || f.title || f.name}</strong>
                      <span className="recording-meta">
                        {f.channel_name ? <span className="recording-channel">{f.channel_name}</span> : null}
                        <span className="muted">
                          {f.recording
                            ? 'Recording in progress…'
                            : `${formatBytes(f.size_bytes)} · ${formatWhen(f.mod_time, { date: true })}`}
                          {f.category ? ` · ${f.category}` : ''}
                        </span>
                      </span>
                      {f.description ? <p className="recording-epg-desc muted">{f.description}</p> : null}
                      <span className="mono muted recording-path">{f.name}</span>
                    </div>
                    <div className="recording-row-actions">
                      {f.recording && liveJob ? (
                        <button
                          type="button"
                          className="ghost"
                          disabled={busy === f.name || busy === liveJob.channel_id}
                          onClick={() => void stopChannel(liveJob.channel_id, f.name)}
                        >
                          {busy === f.name ? 'Stopping…' : 'Stop'}
                        </button>
                      ) : null}
                      <button
                        type="button"
                        disabled={f.recording}
                        onClick={() => playRecording(f.name, f.program_title || f.title || f.name)}
                        title={f.recording ? 'Wait until recording finishes' : 'Play'}
                      >
                        <Icon icon={icons.play} /> Play
                      </button>
                      <button
                        type="button"
                        className="ghost"
                        disabled={f.recording || busy === f.name}
                        onClick={async () => {
                          if (!window.confirm(`Delete ${f.name}?`)) return
                          setBusy(f.name)
                          setError('')
                          try {
                            await api.deleteRecording(f.name)
                            notifyLiveRecordingChange()
                            refresh()
                          } catch (err) {
                            setError(err instanceof Error ? err.message : 'Delete failed')
                          } finally {
                            setBusy(null)
                          }
                        }}
                      >
                        Delete
                      </button>
                    </div>
                  </div>
                )
              })}
            </div>
          )}
        </section>
      )}
    </div>
  )
}

function Home() {
  const { playLive } = usePlayer()
  const [movies, setMovies] = useState<VodMovie[]>([])
  const [shows, setShows] = useState<VodSeries[]>([])
  const [channels, setChannels] = useState<LiveChannel[]>([])
  const [error, setError] = useState('')

  useEffect(() => {
    Promise.all([
      api.vodMovies({ sort: 'recent', limit: 18 }),
      api.vodSeriesList({ sort: 'recent', limit: 18 }),
      api.liveChannels({ limit: 18 }).catch(() => ({ channels: [] as LiveChannel[] })),
    ])
      .then(([m, s, live]) => {
        setMovies(m.movies ?? [])
        setShows(s.series ?? [])
        setChannels(live.channels ?? [])
      })
      .catch((err) => setError(err instanceof Error ? err.message : 'Failed to load'))
  }, [])

  return (
    <div className="home">
      {error && <div className="error">{error}</div>}
      <section>
        <div className="section-head">
          <h1>
            <Icon icon={icons.film} className="section-icon" /> Recently added movies
          </h1>
          <Link to="/movies" className="section-link">
            See all
          </Link>
        </div>
        <div className="poster-row">
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
          {!movies.length && (
            <p className="muted">
              No movies yet. Import VOD categories in <Link to="/settings">Settings</Link>.
            </p>
          )}
        </div>
      </section>
      <section>
        <div className="section-head">
          <h1>
            <Icon icon={icons.tv} className="section-icon" /> Recently added TV shows
          </h1>
          <Link to="/shows" className="section-link">
            See all
          </Link>
        </div>
        <div className="poster-row">
          {shows.map((s) => (
            <PosterCard
              key={s.id}
              to={`/shows/${s.id}`}
              title={s.name}
              subtitle={[s.year, s.category_name].filter(Boolean).join(' · ')}
              poster={s.poster_url}
              rating={s.rating}
            />
          ))}
          {!shows.length && (
            <p className="muted">
              No TV shows yet. Import series categories in <Link to="/settings">Settings</Link>.
            </p>
          )}
        </div>
      </section>
      <section>
        <div className="section-head">
          <h1>
            <Icon icon={icons.live} className="section-icon" /> Live TV
          </h1>
          <Link to="/live" className="section-link">
            Guide
          </Link>
        </div>
        <div className="poster-row live-row">
          {channels.map((ch) => {
            const logoSrc = liveLogoUrl(ch.logo_url)
            return (
            <button
              key={ch.id}
              type="button"
              className="live-card"
              onClick={() => playLive(ch.id, ch.name)}
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
          {!channels.length && (
            <p className="muted">
              No channels yet. Add an M3U playlist in <Link to="/settings">Settings</Link>.
            </p>
          )}
        </div>
      </section>
    </div>
  )
}

function VodBrowsePage({ kind }: { kind: 'movie' | 'series' }) {
  const [categories, setCategories] = useState<VodCategory[]>([])
  const [category, setCategory] = useState('')
  const [q, setQ] = useState('')
  const [qDebounced, setQDebounced] = useState('')
  const [page, setPage] = useState(1)
  const [pageSize, setPageSizeState] = useState(() => getPageSize())
  const [movies, setMovies] = useState<VodMovie[]>([])
  const [series, setSeries] = useState<VodSeries[]>([])
  const [total, setTotal] = useState(0)
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(true)
  const isMobile = useIsMobile()
  const [catsOpen, setCatsOpen] = useState(false)
  const [drawerDragging, setDrawerDragging] = useState(false)
  const [drawerShift, setDrawerShift] = useState<number | null>(null) // px translateX while dragging
  const drawerShiftRef = useRef<number | null>(null)
  const drawerRef = useRef<HTMLElement>(null)
  const swipeRef = useRef<{
    startX: number
    startY: number
    fromOpen: boolean
    locked: 'h' | 'v' | null
    width: number
  } | null>(null)
  const pinScope = kind === 'movie' ? 'movie' : 'series'
  const { pinned, toggle: togglePin, isPinned } = useCategoryPins(pinScope)
  const catsMin = 160
  const catsMax = 480
  const catsDefault = 260
  const [catsW, setCatsW] = useState(() =>
    loadStoredWidth('stevie.vod.catsW', catsDefault, catsMin, catsMax),
  )
  const catsDrag = useColumnResize(catsW, setCatsW, catsMin, catsMax, 'stevie.vod.catsW')

  useEffect(() => {
    const onSize = () => setPageSizeState(getPageSize())
    window.addEventListener('stevie:page-size', onSize)
    return () => window.removeEventListener('stevie:page-size', onSize)
  }, [])

  useEffect(() => {
    const t = window.setTimeout(() => setQDebounced(q.trim()), 250)
    return () => window.clearTimeout(t)
  }, [q])

  useEffect(() => {
    setPage(1)
  }, [kind, category, qDebounced, pageSize])

  useEffect(() => {
    setCatsOpen(false)
    drawerShiftRef.current = null
    setDrawerShift(null)
    setDrawerDragging(false)
  }, [kind])

  useEffect(() => {
    if (!isMobile) {
      setCatsOpen(false)
      drawerShiftRef.current = null
      setDrawerShift(null)
      setDrawerDragging(false)
    }
  }, [isMobile])

  useEffect(() => {
    api
      .vodImportedCategories(kind)
      .then((r) => setCategories(sortByCategoryName(r.categories ?? [], (c) => c.name)))
      .catch(() => setCategories([]))
  }, [kind])

  const orderedCategories = useMemo(
    () =>
      sortCategoriesWithPins(
        categories,
        pinned,
        (c) => c.external_id,
        (c) => c.name,
      ),
    [categories, pinned],
  )

  const selectedCatLabel = useMemo(() => {
    if (!category) return 'All categories'
    return orderedCategories.find((c) => c.external_id === category)?.name || 'Category'
  }, [category, orderedCategories])

  const setShift = useCallback((v: number | null) => {
    drawerShiftRef.current = v
    setDrawerShift(v)
  }, [])

  const closeCats = useCallback(() => {
    setCatsOpen(false)
    setShift(null)
    setDrawerDragging(false)
  }, [setShift])

  const openCats = useCallback(() => {
    setCatsOpen(true)
    setShift(null)
    setDrawerDragging(false)
  }, [setShift])

  const pickCategory = useCallback(
    (id: string) => {
      setCategory(id)
      if (isMobile) closeCats()
    },
    [isMobile, closeCats],
  )

  useEffect(() => {
    if (!isMobile || !catsOpen) return
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') closeCats()
    }
    const prevOverflow = document.body.style.overflow
    document.body.style.overflow = 'hidden'
    window.addEventListener('keydown', onKey)
    return () => {
      document.body.style.overflow = prevOverflow
      window.removeEventListener('keydown', onKey)
    }
  }, [isMobile, catsOpen, closeCats])

  useEffect(() => {
    if (!isMobile) return
    const edge = 28
    const onStart = (e: TouchEvent) => {
      if (e.touches.length !== 1) return
      const t = e.touches[0]
      const width = Math.min(window.innerWidth * 0.86, 320)
      if (catsOpen) {
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
          setDrawerDragging(false)
          setShift(null)
          return
        }
        setDrawerDragging(true)
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
      const shift = drawerShiftRef.current
      if (s.locked !== 'h' || shift == null) {
        setDrawerDragging(false)
        setShift(null)
        return
      }
      const visible = width + shift
      const openEnough = visible > width * 0.35
      setCatsOpen(openEnough)
      // Drop the drag offset on the next frame so CSS open/closed state is already applied.
      requestAnimationFrame(() => {
        setDrawerDragging(false)
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
  }, [isMobile, catsOpen, openCats, closeCats, setShift])

  useEffect(() => {
    setLoading(true)
    setError('')
    const opts = {
      category: category || undefined,
      q: qDebounced || undefined,
      sort: 'name',
      limit: pageSize,
      offset: (page - 1) * pageSize,
    }
    const req =
      kind === 'movie'
        ? api.vodMovies(opts).then((r) => {
            setMovies(r.movies ?? [])
            setSeries([])
            setTotal(r.total ?? 0)
          })
        : api.vodSeriesList(opts).then((r) => {
            setSeries(r.series ?? [])
            setMovies([])
            setTotal(r.total ?? 0)
          })
    req.catch((err) => setError(err instanceof Error ? err.message : 'Failed to load')).finally(() => setLoading(false))
  }, [kind, category, qDebounced, page, pageSize])

  const drawerWidth = typeof window !== 'undefined' ? Math.min(window.innerWidth * 0.86, 320) : 320
  const drawerStyle: CSSProperties | undefined =
    isMobile && drawerShift != null
      ? {
          transform: `translate3d(${drawerShift}px, 0, 0)`,
        }
      : undefined
  const handleStyle: CSSProperties | undefined =
    isMobile && drawerShift != null
      ? {
          transform: `translate3d(${drawerWidth + drawerShift}px, 0, 0)`,
        }
      : undefined

  return (
    <div
      className={['vod-browse', isMobile ? 'is-mobile' : '', catsOpen ? 'cats-open' : '', drawerDragging ? 'cats-dragging' : '']
        .filter(Boolean)
        .join(' ')}
      style={{ ['--vod-cats-w' as string]: `${catsW}px` }}
    >
      {isMobile && (
        <>
          <button
            type="button"
            className={['vod-cats-backdrop', catsOpen || drawerDragging ? 'is-open' : '']
              .filter(Boolean)
              .join(' ')}
            style={
              drawerShift != null
                ? {
                    opacity: Math.min(1, Math.max(0, (drawerWidth + drawerShift) / 280)),
                  }
                : undefined
            }
            aria-label="Close categories"
            onClick={closeCats}
          />
          <button
            type="button"
            className={['vod-cats-handle', catsOpen ? 'is-open' : ''].filter(Boolean).join(' ')}
            style={handleStyle}
            aria-label={catsOpen ? 'Hide categories' : 'Show categories'}
            aria-expanded={catsOpen}
            onClick={() => (catsOpen ? closeCats() : openCats())}
          >
            <Icon icon={catsOpen ? icons.chevronLeft : icons.chevronRight} />
          </button>
        </>
      )}
      <aside
        ref={drawerRef}
        className={['vod-cats', catsOpen ? 'is-open' : ''].filter(Boolean).join(' ')}
        style={drawerStyle}
        aria-hidden={isMobile ? !catsOpen && drawerShift == null : undefined}
      >
        <div className="vod-cats-head">
          <h2>{kind === 'movie' ? 'Movie categories' : 'Series categories'}</h2>
          {isMobile && (
            <button type="button" className="ghost vod-cats-close" onClick={closeCats} aria-label="Close">
              <Icon icon={icons.close} />
            </button>
          )}
        </div>
        <button
          type="button"
          className={!category ? 'vod-cat active' : 'vod-cat'}
          onClick={() => pickCategory('')}
        >
          All <span className="muted">{total}</span>
        </button>
        {orderedCategories.map((c) => (
          <div key={c.external_id} className={category === c.external_id ? 'cat-option active' : 'cat-option'}>
            <button
              type="button"
              className={category === c.external_id ? 'vod-cat active' : 'vod-cat'}
              onClick={() => pickCategory(c.external_id)}
            >
              <span className="vod-cat-name">{c.name}</span>
              <span className="muted">{c.title_count}</span>
            </button>
            <CategoryPinButton
              pinned={isPinned(c.external_id)}
              label={c.name}
              onToggle={() => togglePin(c.external_id)}
            />
          </div>
        ))}
        {!categories.length && <p className="muted">Import categories in Settings.</p>}
      </aside>
      <div
        className="vod-cats-resizer"
        title="Drag to resize categories"
        onPointerDown={catsDrag.onPointerDown}
        onPointerMove={catsDrag.onPointerMove}
        onPointerUp={catsDrag.onPointerUp}
        onPointerCancel={catsDrag.onPointerUp}
      />
      <div className="vod-main">
        <div className="section-head">
          <h1>
            <Icon icon={kind === 'movie' ? icons.film : icons.tv} className="section-icon" />{' '}
            {kind === 'movie' ? 'Movies' : 'TV Shows'}
          </h1>
          {isMobile && (
            <button type="button" className="ghost vod-cat-chip" onClick={openCats}>
              {selectedCatLabel}
            </button>
          )}
          <input
            className="vod-search"
            value={q}
            onChange={(e) => setQ(e.target.value)}
            placeholder="Search…"
            enterKeyHint="search"
          />
          <span className="muted">{total}</span>
        </div>
        {error && <div className="error">{error}</div>}
        {loading ? (
          <p className="muted">Loading…</p>
        ) : (
          <>
            <div className="poster-grid">
              {kind === 'movie'
                ? movies.map((m) => {
                    const tech = vodTechBadges(m)
                    return (
                      <PosterCard
                        key={m.id}
                        to={`/movies/${m.id}`}
                        title={m.name}
                        subtitle={m.year || undefined}
                        poster={m.poster_url}
                        rating={m.rating}
                        badges={tech.slice(0, 3).join(' · ')}
                      />
                    )
                  })
                : series.map((s) => (
                    <PosterCard
                      key={s.id}
                      to={`/shows/${s.id}`}
                      title={s.name}
                      subtitle={s.year || undefined}
                      poster={s.poster_url}
                      rating={s.rating}
                    />
                  ))}
              {!total && (
                <p className="muted">
                  Nothing here yet. Import Xtream VOD in <Link to="/settings">Settings</Link>.
                </p>
              )}
            </div>
            <PaginationBar
              page={page}
              pageSize={pageSize}
              total={total}
              disabled={loading}
              onPageChange={(p) => {
                setPage(p)
                window.scrollTo({ top: 0, behavior: 'smooth' })
              }}
            />
          </>
        )}
      </div>
    </div>
  )
}

function MoviesPage() {
  return <VodBrowsePage kind="movie" />
}

function ShowsPage() {
  return <VodBrowsePage kind="series" />
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

function MovieDetail() {
  const { id } = useParams()
  const { playVod } = usePlayer()
  const navigate = useNavigate()
  const [movie, setMovie] = useState<VodMovie | null>(null)
  const [versions, setVersions] = useState<VodMovie[]>([])
  const [extra, setExtra] = useState<{ country?: string; age?: string }>({})
  const [durationSec, setDurationSec] = useState(0)
  const [error, setError] = useState('')
  const [downloadBusy, setDownloadBusy] = useState(false)
  const [downloadMsg, setDownloadMsg] = useState('')
  const [analyzeBusy, setAnalyzeBusy] = useState(false)
  const [analyze, setAnalyze] = useState<VodAnalyzeResult | null>(null)

  useEffect(() => {
    if (!id) return
    api
      .vodMovie(id)
      .then((r) => {
        setMovie(r.movie)
        setVersions(r.versions ?? [])
        setExtra(r.extra ?? {})
        const fromApi = r.duration_sec && r.duration_sec > 0 ? r.duration_sec : parseDurationSec(r.movie.duration)
        setDurationSec(fromApi)
      })
      .catch((err) => setError(err instanceof Error ? err.message : 'Failed to load'))
  }, [id])

  if (error && !movie) return <div className="error">{error}</div>
  if (!movie) return <div className="muted">Loading…</div>

  const genres = (movie.genre || '')
    .split(/[,|/]/)
    .map((g) => g.trim())
    .filter(Boolean)
  const cast = (movie.cast || '')
    .split(',')
    .map((c) => c.trim())
    .filter(Boolean)
    .slice(0, 12)
    .map((name) => ({ name }))
  const tmdbNum = movie.tmdb_id ? Number(movie.tmdb_id) : 0
  const tech = vodTechBadges(movie)

  const onDownload = async () => {
    setDownloadBusy(true)
    setDownloadMsg('')
    setError('')
    try {
      const r = await api.downloadVodMovie(movie.id)
      setDownloadMsg(`Downloading… ${r.download.file_name}`)
      notifyLiveRecordingChange()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Download failed')
    } finally {
      setDownloadBusy(false)
    }
  }

  const onAnalyze = async () => {
    setAnalyzeBusy(true)
    setError('')
    try {
      const r = await api.analyzeVodMovie(movie.id)
      setMovie(r.movie)
      setAnalyze(r)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Analyze failed')
    } finally {
      setAnalyzeBusy(false)
    }
  }

  return (
    <>
      <DetailShell
        backdrop={movie.backdrop_url || movie.poster_url}
        poster={movie.poster_url}
        title={movie.name}
        overview={movie.plot}
        meta={[
          movie.year || movie.release_date?.slice(0, 4),
          movie.duration,
          movie.category_name,
          extra.country,
          extra.age ? `Age ${extra.age}` : undefined,
        ]}
        techTags={tech}
        tmdb={
          tmdbNum > 0 && movie.rating
            ? { rating: movie.rating, url: `https://www.themoviedb.org/movie/${tmdbNum}` }
            : movie.rating
              ? { rating: movie.rating, url: '' }
              : undefined
        }
        genres={genres}
        cast={cast}
        crew={movie.director ? movie.director.split(',').map((d) => d.trim()).filter(Boolean) : undefined}
        media={null}
        showTech={false}
        onPlay={() =>
          playVod(
            api.vodMovieRemuxUrl(movie.id),
            movie.name,
            durationSec > 0 ? durationSec : parseDurationSec(movie.duration) || undefined,
          )
        }
        onDownload={onDownload}
        downloadBusy={downloadBusy}
        onAnalyze={onAnalyze}
        analyzeBusy={analyzeBusy}
      />
      {error && <div className="error" style={{ margin: '0.75rem 1rem' }}>{error}</div>}
      {downloadMsg && (
        <p className="ok" style={{ margin: '0.75rem 1rem' }}>
          {downloadMsg}{' '}
          <button type="button" className="ghost" onClick={() => navigate('/recordings')}>
            View Recordings
          </button>
        </p>
      )}
      {analyze && (
        <AnalyzeReportModal
          title={movie.name}
          result={analyze}
          onClose={() => setAnalyze(null)}
        />
      )}
      {versions.length > 0 && (
        <section className="vod-versions">
          <h2>Other versions</h2>
          <p className="muted">Same title on the panel with a different encode or quality.</p>
          <div className="poster-grid">
            {versions.map((v) => (
              <PosterCard
                key={v.id}
                to={`/movies/${v.id}`}
                title={v.name}
                subtitle={v.year || undefined}
                poster={v.poster_url}
                rating={v.rating}
                badges={vodTechBadges(v).slice(0, 4).join(' · ')}
              />
            ))}
          </div>
        </section>
      )}
    </>
  )
}

function ShowDetail() {
  const { id } = useParams()
  const { playVod } = usePlayer()
  const [show, setShow] = useState<VodSeries | null>(null)
  const [episodesBySeason, setEpisodesBySeason] = useState<Record<string, VodEpisode[]>>({})
  const [openEpisodeId, setOpenEpisodeId] = useState<string | null>(null)
  const [error, setError] = useState('')

  useEffect(() => {
    if (!id) return
    api
      .vodSeries(id)
      .then((r) => {
        setShow(r.series)
        setEpisodesBySeason(r.episodes ?? {})
      })
      .catch((err) => setError(err instanceof Error ? err.message : 'Failed to load'))
  }, [id])

  const seasons = useMemo(() => {
    return Object.entries(episodesBySeason)
      .map(([season, eps]) => [Number(season), eps] as const)
      .sort((a, b) => a[0] - b[0])
  }, [episodesBySeason])

  if (error) return <div className="error">{error}</div>
  if (!show) return <div className="muted">Loading…</div>

  const genres = (show.genre || '')
    .split(/[,|/]/)
    .map((g) => g.trim())
    .filter(Boolean)
  const cast = (show.cast || '')
    .split(',')
    .map((c) => c.trim())
    .filter(Boolean)
    .slice(0, 12)
    .map((name) => ({ name }))
  const tmdbNum = show.tmdb_id ? Number(show.tmdb_id) : 0
  const epCount = seasons.reduce((n, [, eps]) => n + eps.length, 0)

  return (
    <>
      <DetailShell
        backdrop={show.backdrop_url || show.poster_url}
        poster={show.poster_url}
        title={show.name}
        overview={show.plot}
        meta={[show.year || show.release_date?.slice(0, 4), epCount ? `${epCount} episodes` : undefined, show.category_name]}
        tmdb={
          tmdbNum > 0 && show.rating
            ? { rating: show.rating, url: `https://www.themoviedb.org/tv/${tmdbNum}` }
            : show.rating
              ? { rating: show.rating, url: '' }
              : undefined
        }
        genres={genres}
        cast={cast}
        media={null}
        showTech={false}
      />
      <section className="episodes">
        <h2>Episodes</h2>
        {seasons.map(([seasonNumber, eps]) => (
          <div key={seasonNumber} className="season-block">
            <h3>Season {seasonNumber}</h3>
            <div className="episode-list">
              {eps.map((ep) => {
                const epId = String(ep.id)
                const open = openEpisodeId === epId
                const code = `S${String(seasonNumber).padStart(2, '0')}E${String(ep.episode_num ?? 0).padStart(2, '0')}`
                return (
                  <div key={epId} className={open ? 'episode-card open' : 'episode-card'}>
                    <button
                      type="button"
                      className="episode-row"
                      onClick={() => setOpenEpisodeId((cur) => (cur === epId ? null : epId))}
                      aria-expanded={open}
                    >
                      <span className="ep-code">{code}</span>
                      <span className="ep-title">{ep.title || `Episode ${ep.episode_num ?? ''}`}</span>
                      <span className="tech-badge">{ep.container_extension || ''}</span>
                    </button>
                    {open && (
                      <div className="episode-expand">
                        <div className="episode-expand-main">
                          {ep.info?.movie_image ? (
                            <img className="episode-still" src={posterUrl(ep.info.movie_image, 'w300')} alt="" />
                          ) : (
                            <div className="episode-still fallback" />
                          )}
                          <div className="episode-copy">
                            <p className="overview">{ep.info?.plot || 'No synopsis available for this episode.'}</p>
                            <div className="detail-play-row">
                              <button
                                type="button"
                                className="play-btn"
                                onClick={() =>
                                  playVod(
                                    api.vodEpisodeRemuxUrl(show.id, epId, ep.container_extension),
                                    `${show.name} · ${code}`,
                                    parseDurationSec(ep.info?.duration_secs),
                                  )
                                }
                              >
                                <Icon icon={icons.play} /> Play
                              </button>
                              <button
                                type="button"
                                className="play-btn ghost"
                                onClick={async () => {
                                  try {
                                    await api.downloadVodEpisode(show.id, epId, ep.container_extension)
                                    notifyLiveRecordingChange()
                                    setError('')
                                  } catch (err) {
                                    setError(err instanceof Error ? err.message : 'Download failed')
                                  }
                                }}
                                title="Save to Recordings"
                              >
                                <Icon icon={icons.download} /> Download
                              </button>
                            </div>
                          </div>
                        </div>
                      </div>
                    )}
                  </div>
                )
              })}
            </div>
          </div>
        ))}
        {!seasons.length && <p className="muted">No episodes found for this show.</p>}
      </section>
    </>
  )
}

function AnalyzeReportModal({
  title,
  result,
  onClose,
}: {
  title: string
  result: VodAnalyzeResult
  onClose: () => void
}) {
  const tech = vodTechBadges({
    resolution: result.tech.resolution || result.movie.resolution,
    video_codec: result.tech.video_codec || result.movie.video_codec,
    audio_codec: result.tech.audio_codec || result.movie.audio_codec,
    source_quality: result.tech.source || result.movie.source_quality,
    hdr: result.tech.hdr || result.movie.hdr,
    container: result.movie.container,
    bitrate_kbps: result.tech.bitrate_kbps || result.movie.bitrate_kbps,
  })

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose()
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [onClose])

  return (
    <div className="modal-backdrop" role="presentation" onClick={onClose}>
      <div
        className="modal analyze-modal"
        role="dialog"
        aria-modal="true"
        aria-label="MediaInfo report"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="modal-head">
          <div>
            <h2>MediaInfo report</h2>
            <p className="muted">{title}</p>
          </div>
          <button type="button" className="ghost" onClick={onClose} title="Close">
            <Icon icon={icons.close} />
          </button>
        </div>
        <div className="meta-row tech-tags">
          {tech.map((t) => (
            <span key={t} className="tech-chip">
              {t}
            </span>
          ))}
        </div>
        <p className="muted analyze-sample-meta">
          Sampled {formatBytes(result.sample_bytes)} (~{Math.round(result.sample_sec)}s)
          {result.streams?.length ? ` · ${result.streams.length} streams` : ''}
        </p>
        {!!result.streams?.length && (
          <div className="analyze-streams">
            <table>
              <thead>
                <tr>
                  <th>#</th>
                  <th>Type</th>
                  <th>Codec</th>
                  <th>Details</th>
                </tr>
              </thead>
              <tbody>
                {result.streams.map((s) => (
                  <tr key={`${s.codec_type}-${s.index}`}>
                    <td className="mono">{s.index}</td>
                    <td>{s.codec_type}</td>
                    <td className="mono">{s.codec_name || '—'}</td>
                    <td className="muted">
                      {[
                        s.width && s.height ? `${s.width}×${s.height}` : '',
                        s.fps,
                        s.channels ? `${s.channels}ch` : '',
                        s.channel_layout,
                        s.language,
                        s.profile,
                      ]
                        .filter(Boolean)
                        .join(' · ') || '—'}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
        <pre className="analyze-report mono">{result.report_text}</pre>
      </div>
    </div>
  )
}

function FileVersionChip({
  file,
  active,
  onClick,
}: {
  file: MediaFile
  active: boolean
  onClick: () => void
}) {
  return (
    <button type="button" className={active ? 'version-chip active' : 'version-chip'} onClick={onClick}>
      <span className="version-text">{mediaVersionLabel(file)}</span>
      <HdrBadges
        compact
        flags={{
          dolby_vision: file.dolby_vision,
          hdr10: file.hdr10,
          hdr10_plus: file.hdr10_plus,
          hlg: file.hlg,
        }}
      />
    </button>
  )
}

function DetailShell({
  backdrop,
  poster,
  title,
  tagline,
  overview,
  meta,
  techTags,
  tmdb,
  genres,
  cast,
  crew,
  media,
  mediaFiles,
  onSelectMedia,
  onPlay,
  onDownload,
  downloadBusy,
  onAnalyze,
  analyzeBusy,
  showTech = true,
}: {
  backdrop?: string
  poster?: string
  title: string
  tagline?: string
  overview?: string
  meta: (string | undefined)[]
  techTags?: string[]
  tmdb?: { rating: number; url: string }
  genres: string[]
  cast: { name: string; detail?: string; portrait?: string }[]
  crew?: string[]
  media: MediaFile | null
  mediaFiles?: MediaFile[]
  onSelectMedia?: (fileId: string) => void
  onPlay?: () => void
  onDownload?: () => void
  downloadBusy?: boolean
  onAnalyze?: () => void
  analyzeBusy?: boolean
  showTech?: boolean
}) {
  return (
    <article className="detail">
      <div
        className="detail-hero"
        style={
          backdrop
            ? {
                backgroundImage: `linear-gradient(180deg, var(--hero-overlay-top), var(--hero-overlay-bottom)), url(${posterUrl(backdrop, 'w1280')})`,
              }
            : undefined
        }
      >
        <div className="detail-hero-inner">
          <div className="detail-poster">
            {poster ? <img src={posterUrl(poster, 'w500')} alt="" /> : <div className="poster-fallback tall" />}
          </div>
          <div className="detail-copy">
            <h1>{title}</h1>
            {tagline && <p className="tagline">{tagline}</p>}
            <div className="meta-row">
              {meta.filter(Boolean).map((m) => (
                <span key={String(m)}>{m}</span>
              ))}
              {tmdb && tmdb.rating > 0 && (
                tmdb.url ? (
                  <a
                    className="tmdb-rating"
                    href={tmdb.url}
                    target="_blank"
                    rel="noopener noreferrer"
                    title="Open on TMDB"
                  >
                    {tmdb.rating.toFixed(1)} TMDB
                  </a>
                ) : (
                  <span className="tmdb-rating">{tmdb.rating.toFixed(1)}</span>
                )
              )}
            </div>
            {!!techTags?.length && (
              <div className="meta-row tech-tags">
                {techTags.map((t) => (
                  <span key={t} className="tech-chip">
                    {t}
                  </span>
                ))}
              </div>
            )}
            <div className="genre-row">
              {genres.map((g) => (
                <span key={g}>{g}</span>
              ))}
            </div>
            {overview && <p className="overview">{overview}</p>}
            {crew && crew.length > 0 && <p className="muted">Directed by {crew.join(', ')}</p>}
            {mediaFiles && mediaFiles.length > 1 && onSelectMedia && (
              <div className="version-row">
                {mediaFiles.map((f) => (
                  <FileVersionChip
                    key={f.id}
                    file={f}
                    active={f.id === media?.id}
                    onClick={() => onSelectMedia(f.id)}
                  />
                ))}
              </div>
            )}
            {(mediaFiles?.length || media) && (
              <div className="detail-play-row">
                <MediaActions
                  files={mediaFiles?.length ? mediaFiles : media ? [media] : []}
                  preferredFileId={media?.id}
                  title={title}
                />
              </div>
            )}
            {(onPlay || onDownload || onAnalyze) && (
              <div className="detail-play-row">
                {onPlay && (
                  <button type="button" className="play-btn" onClick={onPlay}>
                    <Icon icon={icons.play} /> Play
                  </button>
                )}
                {onDownload && (
                  <button
                    type="button"
                    className="play-btn ghost"
                    disabled={downloadBusy}
                    onClick={onDownload}
                    title="Save to Recordings"
                  >
                    <Icon icon={icons.download} /> {downloadBusy ? 'Starting…' : 'Download'}
                  </button>
                )}
                {onAnalyze && (
                  <button
                    type="button"
                    className="play-btn ghost"
                    disabled={analyzeBusy}
                    onClick={onAnalyze}
                    title="Sample stream and run MediaInfo"
                  >
                    <Icon icon={icons.search} /> {analyzeBusy ? 'Analyzing…' : 'Analyze'}
                  </button>
                )}
              </div>
            )}
          </div>
        </div>
      </div>

      {cast.length > 0 && (
        <section className="cast">
          <h2>Cast</h2>
          <div className="cast-grid">
            {cast.map((c) => (
              <div key={c.name} className="cast-card">
                {c.portrait ? (
                  <img className="cast-portrait" src={posterUrl(c.portrait, 'w185')} alt="" loading="lazy" />
                ) : (
                  <div className="cast-portrait fallback" aria-hidden />
                )}
                <div className="cast-copy">
                  <strong>{c.name}</strong>
                  <span className="muted">{c.detail}</span>
                </div>
              </div>
            ))}
          </div>
        </section>
      )}

      {showTech && <TechnicalPanel media={media} />}
    </article>
  )
}

function TechnicalPanel({ media, defaultOpen = false }: { media: MediaFile | null; defaultOpen?: boolean }) {
  const [open, setOpen] = useState(defaultOpen)
  const summary = useMemo(() => {
    if (!media) return ''
    const video = media.streams?.find((s) => s.codec_type === 'video')
    const audio = media.streams?.find((s) => s.codec_type === 'audio')
    return [
      media.container,
      video?.height ? `${video.height}p` : undefined,
      video?.codec_name?.toUpperCase(),
      audio?.codec_name?.toUpperCase(),
      formatBytes(media.size_bytes),
    ]
      .filter(Boolean)
      .join(' · ')
  }, [media])

  if (!media?.id) {
    return (
      <section className="tech-panel">
        <button type="button" className="tech-toggle" disabled>
          Technical details unavailable
        </button>
      </section>
    )
  }

  const videoStreams = media.streams?.filter((s) => s.codec_type === 'video') ?? []
  const audioStreams = media.streams?.filter((s) => s.codec_type === 'audio') ?? []
  const subStreams = media.streams?.filter((s) => s.codec_type === 'subtitle') ?? []

  return (
    <section className="tech-panel">
      <button type="button" className="tech-toggle" onClick={() => setOpen((v) => !v)} aria-expanded={open}>
        <span>{open ? 'Hide technical details' : 'Technical details'}</span>
        <span className="tech-badge">{summary}</span>
      </button>
      {open && (
        <div className="tech-body">
          <div className="tech-path mono" title={media.path?.split(/[/\\]/).pop() || media.path}>
            {media.path?.split(/[/\\]/).pop() || media.path}
          </div>
          <div className="tech-stats">
            <div>
              <span className="muted">Container</span>
              <strong>{media.container ?? media.format_name ?? '—'}</strong>
            </div>
            <div>
              <span className="muted">Size</span>
              <strong>{formatBytes(media.size_bytes)}</strong>
            </div>
            <div>
              <span className="muted">Duration</span>
              <strong>{formatDuration(media.duration_ms)}</strong>
            </div>
            <div>
              <span className="muted">Bitrate</span>
              <strong>{media.bitrate ? `${Math.round(media.bitrate / 1000)} kbps` : '—'}</strong>
            </div>
          </div>

          {videoStreams.length > 0 && (
            <div className="stream-block">
              <h3>Video</h3>
              <div className="stream-cards">
                {videoStreams.map((s) => (
                  <div key={s.stream_index} className="stream-card">
                    <div className="stream-top">
                      <strong>{s.codec_name?.toUpperCase() || 'VIDEO'}</strong>
                      <span className="muted">#{s.stream_index}</span>
                      {media && (
                        <HdrBadges
                          compact
                          flags={{
                            dolby_vision: media.dolby_vision,
                            hdr10: media.hdr10,
                            hdr10_plus: media.hdr10_plus,
                            hlg: media.hlg,
                          }}
                        />
                      )}
                    </div>
                    <div className="stream-meta">
                      {[
                        s.width && s.height ? `${s.width}×${s.height}` : null,
                        s.profile,
                        s.pix_fmt,
                        s.color_transfer,
                        s.fps ? `${s.fps} fps` : null,
                        s.bit_depth ? `${s.bit_depth}-bit` : null,
                      ]
                        .filter(Boolean)
                        .join(' · ')}
                    </div>
                  </div>
                ))}
              </div>
            </div>
          )}

          {audioStreams.length > 0 && (
            <div className="stream-block">
              <h3>Audio</h3>
              <div className="stream-cards">
                {audioStreams.map((s) => (
                  <div key={s.stream_index} className="stream-card">
                    <div className="stream-top">
                      <LangFlag lang={s.language} />
                      <strong>{s.codec_name?.toUpperCase() || 'AUDIO'}</strong>
                      <span className="muted">#{s.stream_index}</span>
                    </div>
                    <div className="stream-meta">
                      {[
                        s.channel_layout || (s.channels ? `${s.channels}ch` : null),
                        s.bit_rate ? `${Math.round(s.bit_rate / 1000)} kbps` : null,
                        s.title,
                        s.disposition_default ? 'default' : null,
                      ]
                        .filter(Boolean)
                        .join(' · ')}
                    </div>
                  </div>
                ))}
              </div>
            </div>
          )}

          {subStreams.length > 0 && (
            <div className="stream-block">
              <h3>Subtitles</h3>
              <div className="sub-row">
                {subStreams.map((s) => (
                  <div key={s.stream_index} className="sub-chip" title={s.title || s.codec_name || ''}>
                    <LangFlag lang={s.language} />
                    <span className="muted">{s.codec_name}</span>
                    {s.disposition_forced && <span className="flag-tag">forced</span>}
                    {s.disposition_default && <span className="flag-tag">default</span>}
                    {s.disposition_hearing_impaired && <span className="flag-tag">HI</span>}
                  </div>
                ))}
              </div>
            </div>
          )}
        </div>
      )}
    </section>
  )
}

function Settings({ theme, onThemeChange }: { theme: Theme; onThemeChange: (t: Theme) => void }) {
  const { prefs, setUseLocalTime, setTimeZone } = useTimePrefs()
  const browserTz = detectBrowserTimeZone()
  const tzOptions = useMemo(() => {
    const base = [...TIMEZONE_OPTIONS]
    if (browserTz && !base.some((o) => o.value === browserTz)) {
      base.splice(1, 0, { value: browserTz, label: `Browser (${browserTz})` })
    }
    return base
  }, [browserTz])
  const [status, setStatus] = useState<Status | null>(null)
  const [browsePageSize, setBrowsePageSize] = useState(() => getPageSize())
  const [live, setLive] = useState<LiveSettings | null>(null)
  const [m3uSource, setM3uSource] = useState('')
  const [xmltvSource, setXmltvSource] = useState('')
  const [error, setError] = useState('')
  const [message, setMessage] = useState('')
  const [liveBusy, setLiveBusy] = useState(false)
  const [xtreamOpen, setXtreamOpen] = useState(false)
  const [xtreamCats, setXtreamCats] = useState<XtreamCategory[]>([])
  const [xtreamSelected, setXtreamSelected] = useState<Set<string>>(new Set())
  const [xtreamFilter, setXtreamFilter] = useState('')
  const [xtreamLoading, setXtreamLoading] = useState(false)
  const [xtreamSync, setXtreamSync] = useState<XtreamSyncProgress | null>(null)
  const [epgSync, setEpgSync] = useState<EPGSyncProgress | null>(null)
  const [vodOpen, setVodOpen] = useState(false)
  const [vodTab, setVodTab] = useState<'movie' | 'series'>('movie')
  const [vodMovieCats, setVodMovieCats] = useState<XtreamCategory[]>([])
  const [vodSeriesCats, setVodSeriesCats] = useState<XtreamCategory[]>([])
  const [vodMovieSelected, setVodMovieSelected] = useState<Set<string>>(new Set())
  const [vodSeriesSelected, setVodSeriesSelected] = useState<Set<string>>(new Set())
  const [vodFilter, setVodFilter] = useState('')
  const [vodLoading, setVodLoading] = useState(false)
  const [vodSync, setVodSync] = useState<VodSyncProgress | null>(null)
  const [showPlayerTechInfo, setShowPlayerTechInfoState] = useState(getShowPlayerTechInfo)

  const refresh = () => {
    api.status().then(setStatus)
    api.liveSettings().then((s) => {
      setLive(s)
      setM3uSource(s.m3u_source || '')
      setXmltvSource(s.xmltv_source || '')
      if (s.xtream_sync) setXtreamSync(s.xtream_sync)
      if (s.epg_sync) setEpgSync(s.epg_sync)
      if (s.vod_sync) setVodSync(s.vod_sync)
    })
  }

  useEffect(() => {
    refresh()
  }, [])

  useEffect(() => {
    if (!xtreamSync || xtreamSync.done) return
    const timer = window.setInterval(async () => {
      try {
        const p = await api.xtreamSyncProgress()
        setXtreamSync(p)
        if (p.done) {
          refresh()
          if (p.phase === 'done') {
            setMessage(p.message || 'Xtream sync complete')
          } else if (p.error) {
            setError(p.error)
          }
        }
      } catch {
        /* ignore transient poll errors */
      }
    }, 1000)
    return () => window.clearInterval(timer)
  }, [xtreamSync?.done, xtreamSync?.phase])

  useEffect(() => {
    if (!epgSync || epgSync.done) return
    const timer = window.setInterval(async () => {
      try {
        const p = await api.epgRefreshProgress()
        setEpgSync(p)
        if (p.done) {
          refresh()
          if (p.phase === 'done') {
            setMessage(p.message || 'EPG refreshed')
          } else if (p.error) {
            setError(p.error)
          }
        }
      } catch {
        /* ignore transient poll errors */
      }
    }, 750)
    return () => window.clearInterval(timer)
  }, [epgSync?.done, epgSync?.phase])

  useEffect(() => {
    if (!vodSync || vodSync.done) return
    const timer = window.setInterval(async () => {
      try {
        const p = await api.vodSyncProgress()
        setVodSync(p)
        if (p.done) {
          refresh()
          if (p.phase === 'done') {
            setMessage(p.message || 'VOD sync complete')
          } else if (p.error) {
            setError(p.error)
          }
        }
      } catch {
        /* ignore */
      }
    }, 1000)
    return () => window.clearInterval(timer)
  }, [vodSync?.done, vodSync?.phase])

  const openXtreamModal = async () => {
    setXtreamOpen(true)
    setXtreamLoading(true)
    setError('')
    try {
      const r = await api.xtreamCategories()
      setXtreamCats(sortByCategoryName(r.categories ?? [], (c) => c.category_name || ''))
      setXtreamSelected(new Set(r.imported ?? []))
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load Xtream categories')
      setXtreamOpen(false)
    } finally {
      setXtreamLoading(false)
    }
  }

  const openVodModal = async () => {
    setVodOpen(true)
    setVodLoading(true)
    setError('')
    try {
      const [movies, series] = await Promise.all([api.vodCategories('movie'), api.vodCategories('series')])
      setVodMovieCats(sortByCategoryName(movies.categories ?? [], (c) => c.category_name || ''))
      setVodSeriesCats(sortByCategoryName(series.categories ?? [], (c) => c.category_name || ''))
      setVodMovieSelected(new Set(movies.imported ?? []))
      setVodSeriesSelected(new Set(series.imported ?? []))
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load VOD categories')
      setVodOpen(false)
    } finally {
      setVodLoading(false)
    }
  }

  const filteredXtreamCats = useMemo(() => {
    const q = xtreamFilter.trim().toLowerCase()
    const list = q
      ? xtreamCats.filter((c) => (c.category_name || '').toLowerCase().includes(q))
      : xtreamCats
    return sortByCategoryName(list, (c) => c.category_name || '')
  }, [xtreamCats, xtreamFilter])

  const filteredVodCats = useMemo(() => {
    const list = vodTab === 'movie' ? vodMovieCats : vodSeriesCats
    const q = vodFilter.trim().toLowerCase()
    const filtered = q
      ? list.filter((c) => (c.category_name || '').toLowerCase().includes(q))
      : list
    return sortByCategoryName(filtered, (c) => c.category_name || '')
  }, [vodTab, vodMovieCats, vodSeriesCats, vodFilter])

  const vodSelected = vodTab === 'movie' ? vodMovieSelected : vodSeriesSelected
  const setVodSelected = vodTab === 'movie' ? setVodMovieSelected : setVodSeriesSelected

  const syncBusy = !!(xtreamSync && !xtreamSync.done)
  const epgBusy = !!(epgSync && !epgSync.done)
  const vodBusy = !!(vodSync && !vodSync.done)

  const epgProgressPct = (() => {
    if (!epgSync) return 0
    if (epgSync.phase === 'done') return 100
    if (epgSync.phase === 'error') return 100
    if (epgSync.phase === 'finalize' || epgSync.phase === 'override') {
      return Math.min(95, 70 + Math.min(25, epgSync.programs_written / 2000))
    }
    if (epgSync.programs_written > 0) {
      return Math.min(88, 25 + Math.min(60, epgSync.programs_written / 1500))
    }
    if (epgSync.phase === 'import' || epgSync.phase === 'clear') return 22
    if (epgSync.phase === 'download') return 12
    return 6
  })()

  return (
    <div className="settings">
      <h1>Settings</h1>
      {status && (
        <div className="status-card">
          <div>
            <span className="muted">Domain</span>
            <strong>
              {status.domain}:{status.https_port}
            </strong>
          </div>
          <div>
            <span className="muted">TMDB</span>
            <strong>{status.tmdb_configured ? 'Configured' : 'Not configured'}</strong>
          </div>
        </div>
      )}

      <section>
        <h2>Appearance</h2>
        <div className="theme-switch" role="group" aria-label="Theme">
          <button type="button" className={theme === 'dark' ? 'active' : ''} onClick={() => onThemeChange('dark')}>
            <Icon icon={icons.moon} /> Dark
          </button>
          <button
            type="button"
            className={theme === 'less-dark' ? 'active' : ''}
            onClick={() => onThemeChange('less-dark')}
          >
            <Icon icon={icons.sun} /> Less Dark
          </button>
        </div>
        <p className="muted">Dark is the default. Your choice is saved in this browser.</p>
      </section>

      <section>
        <h2>Time display</h2>
        <label className="settings-toggle">
          <input
            type="checkbox"
            checked={prefs.useLocalTime}
            onChange={(e) => setUseLocalTime(e.target.checked)}
          />
          <span>Use local time</span>
        </label>
        <label className="settings-field">
          <span>Timezone</span>
          <select
            value={prefs.timeZone || browserTz}
            disabled={!prefs.useLocalTime}
            onChange={(e) => setTimeZone(e.target.value)}
          >
            {tzOptions.map((o) => (
              <option key={o.value} value={o.value}>
                {o.label}
              </option>
            ))}
          </select>
        </label>
        <p className="muted">
          Off shows UTC (e.g. 11:30 PM UTC). On shows the selected zone (e.g. 11:30 PM EST). Applies to
          Sports, Live TV, Recordings, and Search. Currently{' '}
          <strong>{effectiveTimeZone(prefs)}</strong>. Saved in this browser.
        </p>
      </section>

      <section>
        <h2>Browsing</h2>
        <label className="settings-field">
          <span>Items per page</span>
          <select
            value={browsePageSize}
            onChange={(e) => {
              const n = Number(e.target.value) || DEFAULT_PAGE_SIZE
              setPageSize(n)
              setBrowsePageSize(getPageSize())
            }}
          >
            {PAGE_SIZE_OPTIONS.map((n) => (
              <option key={n} value={n}>
                {n}
              </option>
            ))}
          </select>
        </label>
        <p className="muted">
          Applies to Movies, TV Shows, and Live TV. Default is {DEFAULT_PAGE_SIZE}. Saved in this browser.
        </p>
      </section>

      <section>
        <h2>Player</h2>
        <label className="settings-toggle">
          <input
            type="checkbox"
            checked={showPlayerTechInfo}
            onChange={(e) => {
              const on = e.target.checked
              setShowPlayerTechInfo(on)
              setShowPlayerTechInfoState(on)
            }}
          />
          <span>Show stream technical info</span>
        </label>
        <p className="muted">
          Buffer, resolution, bitrate, and fps in the Live TV and VOD player headers. On by default; saved in
          this browser.
        </p>
      </section>

      <section>
        <h2>Live TV</h2>
        <div className="status-card live-xtream-status">
          <div>
            <span className="muted">Xtream</span>
            <strong>{live?.xtream_configured ? 'Configured' : 'Not configured'}</strong>
          </div>
          <div>
            <span className="muted">Imported</span>
            <strong>
              {live?.xtream_imported_categories?.length ?? 0} categories · {live?.xtream_channel_count ?? 0} channels
            </strong>
          </div>
        </div>
        <div className="live-settings-actions" style={{ marginBottom: '1rem' }}>
          <button
            type="button"
            disabled={!live?.xtream_configured || liveBusy || epgBusy || syncBusy}
            onClick={openXtreamModal}
          >
            Refresh Xtream
          </button>
          <button
            type="button"
            className="ghost"
            disabled={liveBusy || epgBusy || syncBusy}
            onClick={async () => {
              setError('')
              setMessage('')
              try {
                await api.startEPGRefresh()
                setEpgSync({
                  phase: 'starting',
                  programs_written: 0,
                  message: 'Starting EPG refresh…',
                  done: false,
                })
              } catch (err) {
                setError(err instanceof Error ? err.message : 'EPG refresh failed')
              }
            }}
          >
            {epgBusy ? 'Refreshing EPG…' : 'Refresh EPG'}
          </button>
        </div>
        {epgBusy && epgSync && (
          <div className="xtream-progress">
            <div className="xtream-progress-head">
              <strong>EPG · {epgSync.phase}</strong>
              <span className="muted">{epgSync.message}</span>
            </div>
            <div className="xtream-progress-bar">
              <div style={{ width: `${epgProgressPct}%` }} />
            </div>
            <p className="muted">
              {epgSync.programs_written > 0
                ? `${epgSync.programs_written.toLocaleString()} programmes imported`
                : 'Waiting on provider download / parse…'}
            </p>
          </div>
        )}
        {syncBusy && xtreamSync && (
          <div className="xtream-progress">
            <div className="xtream-progress-head">
              <strong>{xtreamSync.phase}</strong>
              <span className="muted">{xtreamSync.message}</span>
            </div>
            <div className="xtream-progress-bar">
              <div
                style={{
                  width: `${Math.min(
                    100,
                    xtreamSync.phase === 'done'
                      ? 100
                      : xtreamSync.channels_written > 0
                        ? 60 + Math.min(35, xtreamSync.programs_written / 1000)
                        : xtreamSync.categories_total
                          ? (xtreamSync.categories_done / Math.max(xtreamSync.categories_total, 1)) * 40
                          : 10,
                  )}%`,
                }}
              />
            </div>
            <p className="muted">
              Selected {xtreamSync.selected} · channels {xtreamSync.channels_written} · programmes{' '}
              {xtreamSync.programs_written}
            </p>
          </div>
        )}
        <form
          className="settings-form live-settings"
          onSubmit={async (e) => {
            e.preventDefault()
            setLiveBusy(true)
            setError('')
            setMessage('')
            try {
              const r = await api.saveLiveSettings({
                m3u_source: m3uSource.trim(),
                xmltv_source: xmltvSource.trim(),
                refresh: true,
              })
              setMessage(
                `Saved. Loaded ${r.channels ?? 0} M3U channels` +
                  (r.programs != null ? ` and ${r.programs} programme entries` : '') +
                  '.',
              )
              refresh()
            } catch (err) {
              setError(err instanceof Error ? err.message : 'Failed to save Live TV settings')
            } finally {
              setLiveBusy(false)
            }
          }}
        >
          <label>
            M3U playlist (URL or local path)
            <input
              value={m3uSource}
              onChange={(e) => setM3uSource(e.target.value)}
              placeholder="https://…/playlist.m3u or /live-sources/Live_Playlist.m3u"
            />
          </label>
          <label>
            XMLTV guide override (URL or local path)
            <input
              value={xmltvSource}
              onChange={(e) => setXmltvSource(e.target.value)}
              placeholder="Optional — merges over provider EPG"
            />
          </label>
          <div className="live-settings-actions">
            <button type="submit" disabled={liveBusy || epgBusy || syncBusy}>
              {liveBusy ? 'Saving & refreshing…' : 'Save M3U & refresh EPG'}
            </button>
            <button
              type="button"
              className="ghost"
              disabled={liveBusy || epgBusy || syncBusy}
              onClick={async () => {
                setLiveBusy(true)
                setError('')
                setMessage('')
                try {
                  const r = await api.refreshLive()
                  setMessage(`Refreshed ${r.channels} M3U channels and ${r.programs} programmes.`)
                  refresh()
                } catch (err) {
                  setError(err instanceof Error ? err.message : 'Refresh failed')
                } finally {
                  setLiveBusy(false)
                }
              }}
            >
              Refresh M3U / EPG
            </button>
          </div>
          {live && (
            <p className="muted">
              Currently loaded: {live.channel_count} channels · {live.program_count} programmes.
              {live.live_mount_path
                ? ` Local playlists are read from ${live.live_mount_path} inside the server.`
                : ''}
            </p>
          )}
          <p className="muted">
            Xtream credentials come from <code>XTREAM_*</code> in <code>.env</code>. Provider EPG is used when
            available; the XMLTV URL above merges and overrides matching programmes.
          </p>
        </form>
      </section>

      <section>
        <h2>Movies &amp; TV (Xtream VOD)</h2>
        <div className="status-card live-xtream-status">
          <div>
            <span className="muted">Movies</span>
            <strong>
              {live?.vod_movie_categories?.length ?? 0} categories · {live?.vod_movie_count ?? 0} titles
            </strong>
          </div>
          <div>
            <span className="muted">Series</span>
            <strong>
              {live?.vod_series_categories?.length ?? 0} categories · {live?.vod_series_count ?? 0} titles
            </strong>
          </div>
        </div>
        <div className="live-settings-actions" style={{ marginBottom: '1rem' }}>
          <button type="button" disabled={!live?.xtream_configured || vodBusy} onClick={openVodModal}>
            Import VOD categories
          </button>
        </div>
        {vodBusy && vodSync && (
          <div className="xtream-progress">
            <div className="xtream-progress-head">
              <strong>
                {vodSync.phase}
                {vodSync.kind ? ` · ${vodSync.kind}` : ''}
              </strong>
              <span className="muted">{vodSync.message}</span>
            </div>
            <div className="xtream-progress-bar">
              <div
                style={{
                  width: `${Math.min(
                    100,
                    vodSync.phase === 'done'
                      ? 100
                      : vodSync.categories_total
                        ? (vodSync.categories_done / Math.max(vodSync.categories_total, 1)) * 100
                        : 8,
                  )}%`,
                }}
              />
            </div>
            <p className="muted">
              Selected {vodSync.selected} · titles {vodSync.titles_written} ·{' '}
              {vodSync.categories_done}/{vodSync.categories_total} categories
            </p>
          </div>
        )}
        <p className="muted">
          Stores lightweight metadata (including plot) for selected categories. Series episodes load on demand —
          not bulk-imported. Adult categories are skipped by Select all.
        </p>
      </section>

      {xtreamOpen && (
        <div className="modal-backdrop" role="presentation" onClick={() => !syncBusy && setXtreamOpen(false)}>
          <div
            className="modal xtream-modal"
            role="dialog"
            aria-labelledby="xtream-modal-title"
            onClick={(e) => e.stopPropagation()}
          >
            <div className="modal-head">
              <h2 id="xtream-modal-title">Import Xtream categories</h2>
              <button type="button" className="ghost" disabled={syncBusy} onClick={() => setXtreamOpen(false)}>
                Close
              </button>
            </div>
            {xtreamLoading ? (
              <p className="muted">Loading categories from panel…</p>
            ) : (
              <>
                <div className="xtream-modal-toolbar">
                  <input
                    value={xtreamFilter}
                    onChange={(e) => setXtreamFilter(e.target.value)}
                    placeholder="Filter categories…"
                    aria-label="Filter categories"
                  />
                  <button
                    type="button"
                    className="ghost"
                    onClick={() => setXtreamSelected(new Set(filteredXtreamCats.map((c) => c.category_id)))}
                  >
                    Select all{xtreamFilter ? ' filtered' : ''}
                  </button>
                  <button type="button" className="ghost" onClick={() => setXtreamSelected(new Set())}>
                    Clear
                  </button>
                </div>
                <p className="muted">
                  {xtreamSelected.size} selected · {filteredXtreamCats.length} shown · {xtreamCats.length} total
                </p>
                <div className="xtream-cat-list">
                  {filteredXtreamCats.map((c) => {
                    const checked = xtreamSelected.has(c.category_id)
                    return (
                      <label key={c.category_id} className="xtream-cat-row">
                        <input
                          type="checkbox"
                          checked={checked}
                          onChange={() => {
                            setXtreamSelected((prev) => {
                              const next = new Set(prev)
                              if (next.has(c.category_id)) next.delete(c.category_id)
                              else next.add(c.category_id)
                              return next
                            })
                          }}
                        />
                        <span className="xtream-cat-name">{c.category_name || c.category_id}</span>
                        <span className="muted mono">{c.category_id}</span>
                      </label>
                    )
                  })}
                  {!filteredXtreamCats.length && <p className="muted">No categories match.</p>}
                </div>
                <div className="live-settings-actions">
                  <button
                    type="button"
                    disabled={syncBusy || (!xtreamSelected.size && xtreamCats.length === 0)}
                    onClick={async () => {
                      setError('')
                      setMessage('')
                      try {
                        const selectAll =
                          !xtreamFilter &&
                          xtreamSelected.size === xtreamCats.length &&
                          xtreamCats.length > 0
                        await api.startXtreamSync({
                          category_ids: Array.from(xtreamSelected),
                          select_all: selectAll,
                        })
                        setXtreamSync({
                          phase: 'auth',
                          categories_total: 0,
                          categories_done: 0,
                          channels_written: 0,
                          programs_written: 0,
                          selected: xtreamSelected.size,
                          message: 'Starting…',
                          done: false,
                        })
                        setXtreamOpen(false)
                      } catch (err) {
                        setError(err instanceof Error ? err.message : 'Failed to start Xtream sync')
                      }
                    }}
                  >
                    Import selected
                  </button>
                </div>
              </>
            )}
          </div>
        </div>
      )}

      {vodOpen && (
        <div className="modal-backdrop" role="presentation" onClick={() => !vodBusy && setVodOpen(false)}>
          <div
            className="modal xtream-modal"
            role="dialog"
            aria-labelledby="vod-modal-title"
            onClick={(e) => e.stopPropagation()}
          >
            <div className="modal-head">
              <h2 id="vod-modal-title">Import VOD categories</h2>
              <button type="button" className="ghost" disabled={vodBusy} onClick={() => setVodOpen(false)}>
                Close
              </button>
            </div>
            {vodLoading ? (
              <p className="muted">Loading categories from panel…</p>
            ) : (
              <>
                <p className="muted">
                  Movies and Series are separate imports. Select categories on each tab, then Import selected
                  (both tabs are sent together).
                </p>
                {!vodSeriesSelected.size && !!vodSeriesCats.length && (
                  <p className="error">
                    No series categories selected — the TV Shows tab will stay empty until you import some.
                  </p>
                )}
                <div className="vod-tabs">
                  <button
                    type="button"
                    className={vodTab === 'movie' ? 'active' : ''}
                    onClick={() => {
                      setVodTab('movie')
                      setVodFilter('')
                    }}
                  >
                    Movies ({vodMovieSelected.size}/{vodMovieCats.length})
                  </button>
                  <button
                    type="button"
                    className={vodTab === 'series' ? 'active' : ''}
                    onClick={() => {
                      setVodTab('series')
                      setVodFilter('')
                    }}
                  >
                    Series ({vodSeriesSelected.size}/{vodSeriesCats.length})
                  </button>
                </div>
                <div className="xtream-modal-toolbar">
                  <input
                    value={vodFilter}
                    onChange={(e) => setVodFilter(e.target.value)}
                    placeholder="Filter categories…"
                    aria-label="Filter VOD categories"
                  />
                  <button
                    type="button"
                    className="ghost"
                    onClick={() =>
                      setVodSelected(
                        new Set(
                          filteredVodCats
                            .filter((c) => !/adult|xxx|porn|erotic|\+18|18\+/i.test(c.category_name || ''))
                            .map((c) => c.category_id),
                        ),
                      )
                    }
                  >
                    Select all{vodFilter ? ' filtered' : ''}
                  </button>
                  <button type="button" className="ghost" onClick={() => setVodSelected(new Set())}>
                    Clear
                  </button>
                </div>
                <p className="muted">
                  {vodSelected.size} selected · {filteredVodCats.length} shown ·{' '}
                  {(vodTab === 'movie' ? vodMovieCats : vodSeriesCats).length} total
                </p>
                <div className="xtream-cat-list">
                  {filteredVodCats.map((c) => {
                    const checked = vodSelected.has(c.category_id)
                    return (
                      <label key={c.category_id} className="xtream-cat-row">
                        <input
                          type="checkbox"
                          checked={checked}
                          onChange={() => {
                            setVodSelected((prev) => {
                              const next = new Set(prev)
                              if (next.has(c.category_id)) next.delete(c.category_id)
                              else next.add(c.category_id)
                              return next
                            })
                          }}
                        />
                        <span className="xtream-cat-name">{c.category_name || c.category_id}</span>
                        <span className="muted mono">{c.category_id}</span>
                      </label>
                    )
                  })}
                  {!filteredVodCats.length && <p className="muted">No categories match.</p>}
                </div>
                <div className="live-settings-actions">
                  <button
                    type="button"
                    disabled={vodBusy || (!vodMovieSelected.size && !vodSeriesSelected.size)}
                    onClick={async () => {
                      setError('')
                      setMessage('')
                      try {
                        await api.startVodSync({
                          movie_category_ids: Array.from(vodMovieSelected),
                          series_category_ids: Array.from(vodSeriesSelected),
                        })
                        setVodSync({
                          phase: 'auth',
                          categories_total: 0,
                          categories_done: 0,
                          titles_written: 0,
                          selected: vodMovieSelected.size + vodSeriesSelected.size,
                          message: 'Starting…',
                          done: false,
                        })
                        setVodOpen(false)
                      } catch (err) {
                        setError(err instanceof Error ? err.message : 'Failed to start VOD sync')
                      }
                    }}
                  >
                    Import selected
                  </button>
                </div>
              </>
            )}
          </div>
        </div>
      )}

      {message && <div className="ok">{message}</div>}
      {error && <div className="error">{error}</div>}
    </div>
  )
}
