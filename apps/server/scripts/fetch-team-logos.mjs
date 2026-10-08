#!/usr/bin/env node
/**
 * Download Big-4 (+ CFL) ESPN team logos into apps/server/assets/team-logos/
 * and write manifest.json for Stevie to upsert sports_teams + serve offline.
 */
import { createHash } from 'node:crypto'
import { mkdirSync, writeFileSync, existsSync, readFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'

const __dirname = dirname(fileURLToPath(import.meta.url))
const OUT = join(__dirname, '..', 'assets', 'team-logos')
const ESPN = 'https://site.api.espn.com/apis/site/v2/sports'

/** @type {{ sportPath: string, leaguePath: string, sport: string, league: string }[]} */
const LEAGUES = [
  { sportPath: 'baseball', leaguePath: 'mlb', sport: 'Baseball', league: 'MLB' },
  { sportPath: 'basketball', leaguePath: 'nba', sport: 'Basketball', league: 'NBA' },
  { sportPath: 'hockey', leaguePath: 'nhl', sport: 'Ice Hockey', league: 'NHL' },
  { sportPath: 'football', leaguePath: 'nfl', sport: 'American Football', league: 'NFL' },
  { sportPath: 'football', leaguePath: 'cfl', sport: 'American Football', league: 'CFL' },
  // Extra coverage for Sports tab soccer / college when ESPN lists them.
  { sportPath: 'soccer', leaguePath: 'eng.1', sport: 'Football', league: 'English Premier League' },
  { sportPath: 'soccer', leaguePath: 'usa.1', sport: 'Football', league: 'MLS' },
  { sportPath: 'soccer', leaguePath: 'uefa.champions', sport: 'Football', league: 'UEFA Champions League' },
  { sportPath: 'soccer', leaguePath: 'esp.1', sport: 'Football', league: 'Spanish La Liga' },
  { sportPath: 'soccer', leaguePath: 'ita.1', sport: 'Football', league: 'Italian Serie A' },
  { sportPath: 'soccer', leaguePath: 'ger.1', sport: 'Football', league: 'German Bundesliga' },
  { sportPath: 'soccer', leaguePath: 'fra.1', sport: 'Football', league: 'French Ligue 1' },
  { sportPath: 'basketball', leaguePath: 'wnba', sport: 'Basketball', league: 'WNBA' },
]

const UA = 'Mozilla/5.0 (compatible; StevieSports/1.0)'

async function getJSON(url) {
  const res = await fetch(url, { headers: { 'User-Agent': UA, Accept: 'application/json' } })
  if (!res.ok) throw new Error(`${res.status} ${url}`)
  return res.json()
}

function logoURL(leaguePath, abbr) {
  return `https://a.espncdn.com/i/teamlogos/${leaguePath}/500/${abbr.toLowerCase()}.png`
}

function aliasesFor(name, shortDisplay, abbrev) {
  const out = new Set()
  for (const s of [name, shortDisplay, abbrev]) {
    const t = (s || '').trim()
    if (t) out.add(t)
  }
  // "Chicago White Sox" → also "White Sox"
  const parts = (name || '').trim().split(/\s+/)
  if (parts.length >= 2) {
    out.add(parts.slice(1).join(' '))
    out.add(parts[parts.length - 1])
  }
  return [...out].filter(Boolean)
}

async function download(url, dest) {
  if (existsSync(dest) && readFileSync(dest).length > 500) return true
  const res = await fetch(url, { headers: { 'User-Agent': UA, Accept: 'image/*' } })
  if (!res.ok) return false
  const buf = Buffer.from(await res.arrayBuffer())
  if (buf.length < 200) return false
  writeFileSync(dest, buf)
  return true
}

mkdirSync(OUT, { recursive: true })

/** @type {any[]} */
const manifest = []
const seenKeys = new Set()

for (const lg of LEAGUES) {
  const url = `${ESPN}/${lg.sportPath}/${lg.leaguePath}/teams`
  let data
  try {
    data = await getJSON(url)
  } catch (err) {
    console.warn('skip league', lg.leaguePath, err.message || err)
    continue
  }
  const sports = data?.sports || []
  const leagues = sports[0]?.leagues || []
  const teams = leagues[0]?.teams || []
  console.log(lg.leaguePath, teams.length, 'teams')
  for (const wrap of teams) {
    const t = wrap?.team || wrap
    if (!t) continue
    const abbr = String(t.abbreviation || '').trim()
    const name = String(t.displayName || t.name || '').trim()
    if (!abbr || !name) continue
    const key = `${lg.leaguePath}/${abbr.toLowerCase()}`
    if (seenKeys.has(key)) continue
    const file = `${key}.png`
    const dest = join(OUT, file)
    mkdirSync(dirname(dest), { recursive: true })
    let ok = await download(logoURL(lg.leaguePath, abbr), dest)
    if (!ok && Array.isArray(t.logos) && t.logos[0]?.href) {
      ok = await download(String(t.logos[0].href), dest)
    }
    // ESPN dropped CFL crest CDN — fall back to SportsDB badges.
    if (!ok && lg.leaguePath === 'cfl') {
      const cflBadges = {
        bcl: 'https://r2.thesportsdb.com/images/media/team/badge/ysxssy1424732039.png',
        csp: 'https://r2.thesportsdb.com/images/media/team/badge/o71ce41784719551.png',
        ees: 'https://r2.thesportsdb.com/images/media/team/badge/im99lm1784720368.png',
        htc: 'https://r2.thesportsdb.com/images/media/team/badge/0mrsd41546427902.png',
        mta: 'https://r2.thesportsdb.com/images/media/team/badge/8m9v4n1770835125.png',
        orb: 'https://r2.thesportsdb.com/images/media/team/badge/zy3m9v1784718707.png',
        srr: 'https://r2.thesportsdb.com/images/media/team/badge/xrdull1630952245.png',
        tat: 'https://r2.thesportsdb.com/images/media/team/badge/4t8xtk1784721179.png',
        wbb: 'https://r2.thesportsdb.com/images/media/team/badge/rtputr1421812863.png',
      }
      const badge = cflBadges[abbr.toLowerCase()]
      if (badge) ok = await download(badge, dest)
    }
    if (!ok) {
      console.warn('logo fail', key)
      continue
    }
    seenKeys.add(key)
    manifest.push({
      key,
      file,
      sport: lg.sport,
      league: lg.league,
      name,
      abbr: abbr.toUpperCase(),
      aliases: aliasesFor(name, t.shortDisplayName, abbr),
      external_id: `espn:${lg.leaguePath}:${String(t.id || abbr).toLowerCase()}`,
    })
  }
}

manifest.sort((a, b) => a.key.localeCompare(b.key))
writeFileSync(join(OUT, 'manifest.json'), JSON.stringify({ version: 1, generated_at: new Date().toISOString(), teams: manifest }, null, 2))

let bytes = 0
for (const t of manifest) {
  try {
    bytes += readFileSync(join(OUT, t.file)).length
  } catch {
    /* ignore */
  }
}
console.log(
  JSON.stringify({
    teams: manifest.length,
    bytes,
    mb: Math.round((bytes / 1e6) * 10) / 10,
    out: OUT,
  }),
)
