export type User = {
  id: string
  username: string
  is_admin: boolean
}

export type Library = {
  id: string
  name: string
  root_path: string
  type: 'movie' | 'tv' | 'mixed'
}

export type MediaStream = {
  stream_index: number
  codec_type: string
  codec_name?: string
  profile?: string
  width?: number
  height?: number
  pix_fmt?: string
  fps?: string
  bit_rate?: number
  channels?: number
  channel_layout?: string
  language?: string
  title?: string
  disposition_default: boolean
  disposition_forced: boolean
  disposition_hearing_impaired: boolean
  color_range?: string
  color_space?: string
  color_transfer?: string
  bit_depth?: number
}

export type MediaFile = {
  id: string
  path: string
  size_bytes: number
  container?: string
  duration_ms?: number
  bitrate?: number
  format_name?: string
  format_tags?: Record<string, string>
  streams?: MediaStream[]
  dolby_vision?: boolean
  hdr10?: boolean
  hdr10_plus?: boolean
  hlg?: boolean
  hdr_labels?: string[]
}

export type Movie = {
  id: string
  tmdb_id: number
  title: string
  original_title?: string
  tagline?: string
  overview?: string
  release_date?: string
  runtime_minutes?: number
  vote_average?: number
  vote_count?: number
  poster_path?: string
  backdrop_path?: string
  genres?: { id: number; name: string }[] | unknown
  cast_crew?: {
    cast?: { name: string; character?: string; profile_path?: string }[]
    crew?: { name: string; job?: string }[]
  }
  external_ids?: Record<string, string | null>
  media_file_id?: string
  resolution?: string
  video_codec?: string
  audio_codec?: string
  container?: string
  release_count?: number
}

export type Show = {
  id: string
  tmdb_id: number
  name: string
  tagline?: string
  overview?: string
  first_air_date?: string
  vote_average?: number
  poster_path?: string
  backdrop_path?: string
  genres?: { id: number; name: string }[] | unknown
  cast_crew?: {
    cast?: { name: string; character?: string; profile_path?: string }[]
    crew?: { name: string; job?: string }[]
  }
  episode_count?: number
}

export type Episode = {
  id: string
  season_number: number
  episode_number: number
  name?: string
  overview?: string
  still_path?: string
  air_date?: string
  runtime_minutes?: number
  vote_average?: number
  media_file_id?: string
  resolution?: string
  video_codec?: string
  audio_codec?: string
  container?: string
  release_count?: number
}

export type ScanProgress = {
  library_id: string
  phase: string
  current: number
  total: number
  path?: string
  message?: string
  done: boolean
  error?: string
}

export type Status = {
  domain: string
  https_port: string
  tmdb_configured: boolean
  media_host_path?: string
  media_mount_path?: string
}

export type LiveChannel = {
  id: string
  tvg_id: string
  name: string
  group_title: string
  logo_url: string
  sort_order: number
  source?: string
  external_id?: string
  epg_channel_id?: string
  num?: number
  favorite?: boolean
  created_at?: string
  updated_at?: string
}

export const LIVE_FAVORITES_GROUP = '__favorites__'

export type LiveProgram = {
  id: string
  channel_tvg_id: string
  title: string
  description: string
  start_time: string
  end_time: string
  category: string
}

export type MediaSearchProgramme = {
  id: string
  title: string
  description?: string
  start_time: string
  end_time: string
  category?: string
  channel_id: string
  channel_name: string
  channel_logo?: string
  group_title?: string
  channel_tvg_id?: string
}

export type MediaSearchResult = {
  q: string
  movies: VodMovie[]
  movie_total: number
  series: VodSeries[]
  series_total: number
  channels: LiveChannel[]
  channel_total: number
  programmes: MediaSearchProgramme[]
  programme_total: number
}

export type LiveGuideChannel = LiveChannel & {
  programs: LiveProgram[]
}

export type LiveCategory = {
  id: string
  source: string
  external_id: string
  name: string
  imported: boolean
  channel_count: number
  sort_order: number
}

export type LiveGuide = {
  from: string
  to: string
  groups: string[]
  categories?: LiveCategory[]
  group?: string
  favorites_count?: number
  favorites_group?: string
  channels: LiveGuideChannel[]
  total?: number
  limit?: number
  offset?: number
}

export type XtreamCategory = {
  category_id: string
  category_name: string
  parent_id?: unknown
}

export type XtreamSyncProgress = {
  phase: string
  categories_total: number
  categories_done: number
  channels_written: number
  programs_written: number
  selected: number
  message: string
  error?: string
  done: boolean
  started_at?: string
  updated_at?: string
}

export type EPGSyncProgress = {
  phase: string
  programs_written: number
  message: string
  error?: string
  done: boolean
  started_at?: string
  updated_at?: string
}

export type VodSyncProgress = {
  phase: string
  kind?: string
  categories_total: number
  categories_done: number
  titles_written: number
  selected: number
  message: string
  error?: string
  done: boolean
  started_at?: string
  updated_at?: string
}

export type VodCategory = {
  id: string
  kind: 'movie' | 'series'
  external_id: string
  name: string
  imported: boolean
  title_count: number
  sort_order: number
}

export type VodMovie = {
  id: string
  external_id: string
  category_external_id: string
  category_name?: string
  name: string
  plot?: string
  poster_url?: string
  backdrop_url?: string
  tmdb_id?: string
  rating?: number
  year?: string
  release_date?: string
  genre?: string
  director?: string
  cast?: string
  duration?: string
  container?: string
  trailer?: string
  resolution?: string
  video_codec?: string
  audio_codec?: string
  source_quality?: string
  hdr?: string
  bitrate_kbps?: number
  width?: number
  height?: number
  added_at?: string
}

export type VodSeries = {
  id: string
  external_id: string
  category_external_id: string
  category_name?: string
  name: string
  plot?: string
  poster_url?: string
  backdrop_url?: string
  tmdb_id?: string
  rating?: number
  year?: string
  release_date?: string
  genre?: string
  director?: string
  cast?: string
  episode_run_time?: string
  trailer?: string
  last_modified?: string
}

export type VodEpisode = {
  id: string
  episode_num?: number
  title?: string
  container_extension?: string
  season?: number
  info?: {
    movie_image?: string
    plot?: string
    duration_secs?: number
    rating?: string
    releasedate?: string
  }
}

export type LiveSettings = {
  m3u_source: string
  xmltv_source: string
  channel_count: number
  program_count: number
  live_mount_path?: string
  live_host_path?: string
  xtream_configured?: boolean
  xtream_imported_categories?: string[]
  xtream_channel_count?: number
  xtream_sync?: XtreamSyncProgress
  epg_sync?: EPGSyncProgress
  vod_movie_categories?: string[]
  vod_series_categories?: string[]
  vod_movie_count?: number
  vod_series_count?: number
  vod_sync?: VodSyncProgress
}

export type PlaybackTrack = {
  index: number
  codec?: string
  language?: string
  title?: string
  channels?: number
  default?: boolean
  forced?: boolean
  commentary?: boolean
  text_based?: boolean
  browser_safe?: boolean
}

export type PlaybackDecision = {
  mode: 'direct' | 'remux' | 'unsupported'
  reason?: string
  video_codec?: string
  container?: string
  audio_tracks: PlaybackTrack[]
  subtitle_tracks: PlaybackTrack[]
  default_audio_index: number
  selected_audio_index: number
  transcode_audio: boolean
}

export type PlaybackInfo = {
  media_id: string
  decision: PlaybackDecision
  urls: {
    stream: string
    remux: string
    external: string
  }
}

export type PlaybackToken = {
  token: string
  expires_at: string
  stream_url: string
  remux_url: string
  external_playlist_url: string
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(path, {
    credentials: 'include',
    headers: {
      'Content-Type': 'application/json',
      ...(init?.headers ?? {}),
    },
    ...init,
  })
  if (!res.ok) {
    let message = res.statusText
    try {
      const body = await res.json()
      if (body?.error) message = body.error
    } catch {
      /* ignore */
    }
    throw new Error(message)
  }
  return res.json() as Promise<T>
}

export const api = {
  login: (username: string, password: string) =>
    request<{ user: User }>('/api/auth/login', {
      method: 'POST',
      body: JSON.stringify({ username, password }),
    }),
  logout: () => request<{ status: string }>('/api/auth/logout', { method: 'POST' }),
  me: () => request<{ user: User }>('/api/auth/me'),
  status: () => request<Status>('/api/status'),
  libraries: () => request<{ libraries: Library[] }>('/api/libraries'),
  startScan: (id: string) =>
    request<{ status: string }>(`/api/libraries/${id}/scan`, { method: 'POST' }),
  scanProgress: (id: string) => request<ScanProgress>(`/api/libraries/${id}/scan`),
  movies: (opts?: { sort?: 'title' | 'recent'; limit?: number }) => {
    const q = new URLSearchParams()
    if (opts?.sort) q.set('sort', opts.sort)
    if (opts?.limit) q.set('limit', String(opts.limit))
    const qs = q.toString()
    return request<{ movies: Movie[] }>(`/api/movies${qs ? `?${qs}` : ''}`)
  },
  movie: (id: string, fileId?: string) =>
    request<{ movie: Movie; media: MediaFile; media_files: MediaFile[] }>(
      `/api/movies/${id}${fileId ? `?file=${encodeURIComponent(fileId)}` : ''}`,
    ),
  shows: (opts?: { sort?: 'title' | 'recent'; limit?: number }) => {
    const q = new URLSearchParams()
    if (opts?.sort) q.set('sort', opts.sort)
    if (opts?.limit) q.set('limit', String(opts.limit))
    const qs = q.toString()
    return request<{ shows: Show[] }>(`/api/shows${qs ? `?${qs}` : ''}`)
  },
  show: (id: string) => request<{ show: Show; episodes: Episode[] }>(`/api/shows/${id}`),
  episodeMedia: (id: string) => request<{ media_files: MediaFile[] }>(`/api/episodes/${id}/media`),
  media: (id: string) => request<MediaFile>(`/api/media/${id}`),
  playback: (id: string, query: string) =>
    request<PlaybackInfo>(`/api/media/${id}/playback?${query}`),
  playbackToken: (id: string) =>
    request<PlaybackToken>(`/api/media/${id}/playback-token`, { method: 'POST' }),
  search: (q: string) => request<{ movies: Movie[]; shows: Show[] }>(`/api/search?q=${encodeURIComponent(q)}`),
  mediaSearch: (q: string, limit = 100) => {
    const qs = new URLSearchParams({ q, limit: String(limit) })
    return request<MediaSearchResult>(`/api/search?${qs}`)
  },
  liveSettings: () => request<LiveSettings>('/api/settings/live'),
  saveLiveSettings: (body: { m3u_source: string; xmltv_source: string; refresh?: boolean }) =>
    request<{ m3u_source: string; xmltv_source: string; channels?: number; programs?: number }>(
      '/api/settings/live',
      { method: 'PUT', body: JSON.stringify(body) },
    ),
  refreshLive: () => request<{ channels: number; programs: number }>('/api/live/refresh', { method: 'POST' }),
  startEPGRefresh: () =>
    request<{ status: string }>('/api/live/epg/refresh', { method: 'POST' }),
  epgRefreshProgress: () => request<EPGSyncProgress>('/api/live/epg/refresh'),
  xtreamCategories: () =>
    request<{ categories: XtreamCategory[]; imported: string[] }>('/api/live/xtream/categories'),
  startXtreamSync: (body: { category_ids?: string[]; select_all?: boolean }) =>
    request<{ status: string }>('/api/live/xtream/sync', {
      method: 'POST',
      body: JSON.stringify(body),
    }),
  xtreamSyncProgress: () => request<XtreamSyncProgress>('/api/live/xtream/sync'),
  vodCategories: (kind: 'movie' | 'series') =>
    request<{ kind: string; categories: XtreamCategory[]; imported: string[] }>(
      `/api/vod/xtream/categories?kind=${kind}`,
    ),
  startVodSync: (body: {
    movie_category_ids?: string[]
    series_category_ids?: string[]
    select_all_movies?: boolean
    select_all_series?: boolean
  }) =>
    request<{ status: string }>('/api/vod/xtream/sync', {
      method: 'POST',
      body: JSON.stringify(body),
    }),
  vodSyncProgress: () => request<VodSyncProgress>('/api/vod/xtream/sync'),
  vodImportedCategories: (kind: 'movie' | 'series') =>
    request<{ kind: string; categories: VodCategory[] }>(`/api/vod/categories?kind=${kind}`),
  vodMovies: (opts?: { category?: string; q?: string; sort?: string; limit?: number; offset?: number }) => {
    const q = new URLSearchParams()
    if (opts?.category) q.set('category', opts.category)
    if (opts?.q) q.set('q', opts.q)
    if (opts?.sort) q.set('sort', opts.sort)
    if (opts?.limit) q.set('limit', String(opts.limit))
    if (opts?.offset) q.set('offset', String(opts.offset))
    const qs = q.toString()
    return request<{ movies: VodMovie[]; total: number }>(`/api/vod/movies${qs ? `?${qs}` : ''}`)
  },
  vodMovie: (id: string) =>
    request<{
      movie: VodMovie
      duration_sec?: number
      versions?: VodMovie[]
      extra?: { country?: string; age?: string }
      urls: { remux: string }
    }>(`/api/vod/movies/${id}`),
  vodSeriesList: (opts?: { category?: string; q?: string; sort?: string; limit?: number; offset?: number }) => {
    const q = new URLSearchParams()
    if (opts?.category) q.set('category', opts.category)
    if (opts?.q) q.set('q', opts.q)
    if (opts?.sort) q.set('sort', opts.sort)
    if (opts?.limit) q.set('limit', String(opts.limit))
    if (opts?.offset) q.set('offset', String(opts.offset))
    const qs = q.toString()
    return request<{ series: VodSeries[]; total: number }>(`/api/vod/series${qs ? `?${qs}` : ''}`)
  },
  vodSeries: (id: string) =>
    request<{
      series: VodSeries
      seasons: Record<string, unknown>[]
      episodes: Record<string, VodEpisode[]>
    }>(`/api/vod/series/${id}`),
  vodMovieRemuxUrl: (id: string) => {
    const path = `/api/vod/movies/${id}/remux`
    if (typeof window === 'undefined') return path
    return new URL(path, window.location.origin).href
  },
  vodEpisodeRemuxUrl: (seriesId: string, epId: string, ext?: string) => {
    const q = ext ? `?ext=${encodeURIComponent(ext)}` : ''
    const path = `/api/vod/series/${seriesId}/episodes/${encodeURIComponent(epId)}/remux${q}`
    if (typeof window === 'undefined') return path
    return new URL(path, window.location.origin).href
  },
  downloadVodMovie: (id: string) =>
    request<{ download: VodDownload }>(`/api/vod/movies/${id}/download`, { method: 'POST' }),
  analyzeVodMovie: (id: string) =>
    request<VodAnalyzeResult>(`/api/vod/movies/${id}/analyze`, { method: 'POST' }),
  downloadVodEpisode: (seriesId: string, epId: string, ext?: string) => {
    const q = ext ? `?ext=${encodeURIComponent(ext)}` : ''
    return request<{ download: VodDownload }>(
      `/api/vod/series/${seriesId}/episodes/${encodeURIComponent(epId)}/download${q}`,
      { method: 'POST' },
    )
  },
  vodDownloads: () => request<{ downloads: VodDownload[] }>('/api/vod/downloads'),
  cancelVodDownload: (id: string) =>
    request<{ status: string }>(`/api/vod/downloads/${id}`, { method: 'DELETE' }),
  liveChannels: (opts?: { group?: string; q?: string; limit?: number; source?: string }) => {
    const q = new URLSearchParams()
    if (opts?.group) q.set('group', opts.group)
    if (opts?.q) q.set('q', opts.q)
    if (opts?.limit) q.set('limit', String(opts.limit))
    if (opts?.source) q.set('source', opts.source)
    const qs = q.toString()
    return request<{ channels: LiveChannel[] }>(`/api/live/channels${qs ? `?${qs}` : ''}`)
  },
  liveGroups: () =>
    request<{ groups: string[]; categories?: LiveCategory[]; favorites_count?: number }>(
      '/api/live/groups',
    ),
  liveCategories: () =>
    request<{ categories: LiveCategory[]; favorites_count?: number }>('/api/live/categories'),
  setLiveFavorite: (id: string, favorite: boolean) =>
    request<{ channel: LiveChannel; favorites_count: number }>(`/api/live/channels/${id}/favorite`, {
      method: 'PUT',
      body: JSON.stringify({ favorite }),
    }),
  liveGuide: (opts?: {
    from?: string
    to?: string
    group?: string
    q?: string
    pq?: string
    limit?: number
    offset?: number
    signal?: AbortSignal
  }) => {
    const q = new URLSearchParams()
    if (opts?.from) q.set('from', opts.from)
    if (opts?.to) q.set('to', opts.to)
    if (opts?.group) q.set('group', opts.group)
    if (opts?.q) q.set('q', opts.q)
    if (opts?.pq) q.set('pq', opts.pq)
    if (opts?.limit) q.set('limit', String(opts.limit))
    if (opts?.offset != null) q.set('offset', String(opts.offset))
    const qs = q.toString()
    return request<LiveGuide>(`/api/live/guide${qs ? `?${qs}` : ''}`, {
      signal: opts?.signal,
    })
  },
  liveStreamUrl: (channelId: string) => {
    const path = `/api/live/channels/${channelId}/stream`
    if (typeof window === 'undefined') return path
    return new URL(path, window.location.origin).href
  },
  liveChannel: (id: string) =>
    request<{
      channel: LiveChannel & { stream_url?: string }
      urls: { proxy: string; external: string }
    }>(`/api/live/channels/${id}`),
  startLiveRecording: (id: string, opts?: { program_title?: string }) =>
    request<LiveRecordingJob>(`/api/live/channels/${id}/recording`, {
      method: 'POST',
      body: JSON.stringify({ program_title: opts?.program_title || undefined }),
    }),
  stopLiveRecording: (id: string) =>
    request<LiveRecordingJob>(`/api/live/channels/${id}/recording`, { method: 'DELETE' }),
  liveRecordingStatus: (id: string) =>
    request<{ recording: boolean; job?: LiveRecordingJob }>(`/api/live/channels/${id}/recording`),
  liveRecordings: () =>
    request<{
      active: LiveRecordingJob[]
      files: RecordingFile[]
      scheduled?: ScheduledRecording[]
      downloads?: VodDownload[]
      recordings?: LiveRecordingJob[]
    }>('/api/live/recordings'),
  scheduleRecording: (body: {
    channel_id: string
    program_id?: string
    title: string
    description?: string
    category?: string
    start_time: string
    end_time: string
  }) =>
    request<ScheduledRecording>('/api/live/recordings/schedule', {
      method: 'POST',
      body: JSON.stringify(body),
    }),
  listScheduledRecordings: () =>
    request<{ scheduled: ScheduledRecording[] }>('/api/live/recordings/schedule'),
  cancelScheduledRecording: (id: string) =>
    request<ScheduledRecording>(`/api/live/recordings/schedule/${id}`, { method: 'DELETE' }),
  recordingInfo: (name: string) =>
    request<RecordingFile>(`/api/live/recordings/file/${encodeURIComponent(name)}`),
  deleteRecording: (name: string) =>
    request<{ deleted: boolean; name: string }>(
      `/api/live/recordings/file/${encodeURIComponent(name)}`,
      { method: 'DELETE' },
    ),
  recordingRemuxUrl: (name: string) => {
    const path = `/api/live/recordings/file/${encodeURIComponent(name)}/remux`
    if (typeof window === 'undefined') return path
    return new URL(path, window.location.origin).href
  },
  sportsMeta: () => request<SportsMetaResponse>('/api/sports/meta'),
  sportsEvents: (opts?: { sport?: string; favorites?: string[]; from?: string; to?: string }) => {
    const q = new URLSearchParams()
    if (opts?.sport) q.set('sport', opts.sport)
    if (opts?.favorites?.length) q.set('favorites', opts.favorites.join(','))
    if (opts?.from) q.set('from', opts.from)
    if (opts?.to) q.set('to', opts.to)
    const qs = q.toString()
    return request<{ events: SportsEvent[]; hot?: SportsEvent[] }>(
      `/api/sports/events${qs ? `?${qs}` : ''}`,
    )
  },
  sportsSync: () => request<Record<string, unknown>>('/api/sports/sync', { method: 'POST' }),
  streamedPlay: (q: URLSearchParams) =>
    request<{ stream_url: string; source: string; id: string; stream: string }>(
      `/api/sports/streamed/play?${q}`,
    ),
  startStreamedRecording: (opts: {
    source: string
    id: string
    stream?: number | string
    program_title?: string
    channel_name?: string
  }) => {
    const q = new URLSearchParams({
      source: opts.source,
      id: opts.id,
      stream: String(opts.stream && Number(opts.stream) > 0 ? opts.stream : 1),
    })
    return request<LiveRecordingJob>(`/api/sports/streamed/recording?${q}`, {
      method: 'POST',
      body: JSON.stringify({
        program_title: opts.program_title || undefined,
        channel_name: opts.channel_name || undefined,
      }),
    })
  },
  stopStreamedRecording: (opts: { source: string; id: string; stream?: number | string }) => {
    const q = new URLSearchParams({
      source: opts.source,
      id: opts.id,
      stream: String(opts.stream && Number(opts.stream) > 0 ? opts.stream : 1),
    })
    return request<LiveRecordingJob>(`/api/sports/streamed/recording?${q}`, { method: 'DELETE' })
  },
  streamedRecordingStatus: (opts: { source: string; id: string; stream?: number | string }) => {
    const q = new URLSearchParams({
      source: opts.source,
      id: opts.id,
      stream: String(opts.stream && Number(opts.stream) > 0 ? opts.stream : 1),
    })
    return request<{ recording: boolean; job?: LiveRecordingJob }>(
      `/api/sports/streamed/recording?${q}`,
    )
  },
}

/** Fired when a recording starts/stops so the topbar can refresh immediately. */
export function notifyLiveRecordingChange() {
  try {
    window.dispatchEvent(new Event('stevie:live-recordings'))
  } catch {
    /* ignore */
  }
}

export type SportsTeamSide = {
  name: string
  badge_url?: string
}

export type SportsScore = {
  home: string
  away: string
  period?: string
  status?: string
}

export type SportsChannel = {
  broadcast_label: string
  id?: string
  name?: string
  logo_url?: string
  playable: boolean
  /** True when ESPN/MLB listed this network for the game. */
  confirmed?: boolean
  /** "streamed" for streamed.pk rows. */
  source?: string
  embed_url?: string
  streamed_source?: string
  streamed_id?: string
  streamed_no?: number
}

export type SportsEvent = {
  id: string
  external_id: string
  sport: string
  title: string
  home: SportsTeamSide
  away: SportsTeamSide
  starts_at: string
  ends_at: string
  live: boolean
  upcoming: boolean
  score?: SportsScore | null
  channels: SportsChannel[]
  /** streamed.pk category label when sport is External (streamed.pk). */
  streamed_category?: string
  /** League label (NHL, English Premier League, …). */
  league?: string
  /** Bundled league mark for card watermark. */
  league_logo_url?: string
}

export type SportsMetaResponse = {
  sports: { sport: string; count: number }[]
  sync?: Record<string, unknown>
}

export type LiveRecordingJob = {
  channel_id: string
  channel_name: string
  file_name: string
  path: string
  started_at: string
  status: string
  program_title?: string
  logo_url?: string
  error?: string
}

export type RecordingFile = {
  name: string
  path: string
  size_bytes: number
  mod_time: string
  title: string
  channel_name?: string
  logo_url?: string
  program_title?: string
  description?: string
  category?: string
  duration_ms?: number
  recording: boolean
}

export type VodDownload = {
  id: string
  title: string
  kind: string
  file_name: string
  path: string
  started_at: string
  status: string
  error?: string
  bytes?: number
  total_bytes?: number
  bytes_per_sec?: number
  eta_sec?: number
}

export type VodAnalyzeStream = {
  index: number
  codec_type: string
  codec_name?: string
  profile?: string
  width?: number
  height?: number
  pix_fmt?: string
  fps?: string
  bit_rate?: number
  channels?: number
  channel_layout?: string
  language?: string
  title?: string
  color_transfer?: string
  color_space?: string
}

export type VodAnalyzeResult = {
  movie: VodMovie
  tech: {
    resolution?: string
    video_codec?: string
    audio_codec?: string
    source?: string
    hdr?: string
    bitrate_kbps?: number
    width?: number
    height?: number
  }
  sample_bytes: number
  sample_sec: number
  duration_ms?: number
  bitrate?: number
  streams: VodAnalyzeStream[]
  report_text: string
  report_json?: unknown
}

export type ScheduledRecording = {
  id: string
  channel_id: string
  channel_name: string
  logo_url?: string
  program_id?: string
  title: string
  description?: string
  category?: string
  start_time: string
  end_time: string
  status: string
  error?: string
  created_at?: string
  updated_at?: string
}

/** Same-origin proxy for IPTV logos (avoids mixed-content http:// on https pages). */
export function liveLogoUrl(url?: string | null) {
  if (!url) return ''
  let u = url.trim()
  // Bundled sports crests / already-proxied same-origin paths.
  if (u.startsWith('/api/sports/logo') || u.startsWith('/api/live/logo')) return u
  // Some playlists prefix a stray '-' before http(s).
  if (u.startsWith('-http://') || u.startsWith('-https://')) u = u.slice(1)
  // Never hand file://, absolute paths, or other schemes to <img> — Firefox blocks those
  // from HTTPS pages ("may not load or link to file:///").
  if (!/^https?:\/\//i.test(u)) return ''
  try {
    const parsed = new URL(u)
    if (parsed.protocol !== 'http:' && parsed.protocol !== 'https:') return ''
    if (!parsed.hostname) return ''
  } catch {
    return ''
  }
  return `/api/live/logo?url=${encodeURIComponent(u)}`
}

/** Team crest URL from sports API (bundled `/api/sports/logo` or legacy remote). */
export function sportsBadgeUrl(url?: string | null) {
  return liveLogoUrl(url)
}

export function artworkUrl(path?: string | null, size = 'w500') {
  if (!path) return ''
  return `/api/artwork/poster?path=${encodeURIComponent(path)}&size=${encodeURIComponent(size)}`
}

/** Poster helper: absolute http(s) URLs go through the logo proxy; TMDB paths use artwork. */
export function posterUrl(path?: string | null, size = 'w500') {
  if (!path) return ''
  let p = path.trim()
  // Xtream sometimes stores backdrop as a JSON array string.
  if (p.startsWith('[')) {
    try {
      const arr = JSON.parse(p) as unknown
      if (Array.isArray(arr) && arr.length) p = String(arr[0] ?? '').trim()
    } catch {
      /* ignore */
    }
  }
  if (!p) return ''
  if (p.startsWith('http://') || p.startsWith('https://')) return liveLogoUrl(p)
  return artworkUrl(p, size)
}

export function formatBytes(n?: number) {
  if (n == null || !Number.isFinite(n) || n < 0) return '—'
  if (n === 0) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let v = n
  let i = 0
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024
    i += 1
  }
  return `${v.toFixed(i === 0 ? 0 : 1)} ${units[i]}`
}

export function formatSpeed(bytesPerSec?: number) {
  if (bytesPerSec == null || !Number.isFinite(bytesPerSec) || bytesPerSec <= 0) return ''
  return `${formatBytes(bytesPerSec)}/s`
}

export function formatEta(sec?: number) {
  if (sec == null || !Number.isFinite(sec) || sec <= 0) return ''
  const total = Math.round(sec)
  const h = Math.floor(total / 3600)
  const m = Math.floor((total % 3600) / 60)
  const s = total % 60
  if (h > 0) return `${h}h ${m}m`
  if (m > 0) return `${m}m ${s}s`
  return `${s}s`
}

/** Parse Xtream-style durations: seconds, HH:MM:SS, MM:SS, or "2h 11m". */
export function formatBitrateKbps(kbps?: number) {
  if (!kbps || kbps <= 0) return ''
  if (kbps >= 1000) {
    const mbps = kbps / 1000
    return `${mbps >= 10 ? mbps.toFixed(0) : mbps.toFixed(1)} Mbps`
  }
  return `${kbps} kbps`
}

/** Compact tech badges for poster cards / detail (resolution first). */
export function vodTechBadges(m: {
  resolution?: string
  container?: string
  video_codec?: string
  audio_codec?: string
  source_quality?: string
  hdr?: string
  bitrate_kbps?: number
}) {
  const out: string[] = []
  if (m.resolution) out.push(m.resolution)
  if (m.hdr) out.push(m.hdr)
  if (m.video_codec) out.push(m.video_codec)
  if (m.audio_codec) out.push(m.audio_codec)
  if (m.source_quality) out.push(m.source_quality)
  if (m.container) out.push(m.container.toUpperCase())
  const br = formatBitrateKbps(m.bitrate_kbps)
  if (br) out.push(br)
  return out
}

export function parseDurationSec(v?: string | number | null): number {
  if (typeof v === 'number' && Number.isFinite(v) && v > 0) return v
  if (v == null) return 0
  const raw = String(v).trim()
  if (!raw) return 0
  if (/^\d+(\.\d+)?$/.test(raw)) {
    const n = Number(raw)
    return Number.isFinite(n) && n > 0 ? n : 0
  }
  const hms = raw.match(/^(\d+):([0-5]?\d):([0-5]?\d)$/)
  if (hms) return Number(hms[1]) * 3600 + Number(hms[2]) * 60 + Number(hms[3])
  const ms = raw.match(/^(\d+):([0-5]?\d)$/)
  if (ms) return Number(ms[1]) * 60 + Number(ms[2])
  let sec = 0
  const hours = raw.match(/(\d+)\s*h/i)
  const mins = raw.match(/(\d+)\s*m/i)
  const secs = raw.match(/(\d+)\s*s/i)
  if (hours) sec += Number(hours[1]) * 3600
  if (mins) sec += Number(mins[1]) * 60
  if (secs) sec += Number(secs[1])
  return sec > 0 ? sec : 0
}

export function formatDuration(ms?: number) {
  if (!ms) return '—'
  const total = Math.round(ms / 1000)
  const h = Math.floor(total / 3600)
  const m = Math.floor((total % 3600) / 60)
  const s = total % 60
  if (h > 0) return `${h}h ${m}m`
  return `${m}m ${s}s`
}

export function techBadges(item: {
  resolution?: string
  video_codec?: string
  audio_codec?: string
  container?: string
  release_count?: number
}) {
  if (item.release_count && item.release_count > 1) {
    return `${item.release_count} Releases Available`
  }
  return [item.resolution, item.video_codec?.toUpperCase(), item.audio_codec?.toUpperCase(), item.container]
    .filter(Boolean)
    .join(' · ')
}
