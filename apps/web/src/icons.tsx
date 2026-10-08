import { FontAwesomeIcon } from '@fortawesome/react-fontawesome'
import type { IconDefinition } from '@fortawesome/fontawesome-svg-core'
import {
  faArrowRightFromBracket,
  faBaseballBatBall,
  faBasketball,
  faChevronLeft,
  faChevronRight,
  faClosedCaptioning,
  faCompress,
  faEllipsisVertical,
  faExpand,
  faExternalLink,
  faFilm,
  faFootball,
  faFutbol,
  faGear,
  faGolfBallTee,
  faHardDrive,
  faHockeyPuck,
  faCircle,
  faCirclePlay,
  faDownload,
  faHeadphones,
  faHouse,
  faMagnifyingGlass,
  faMoon,
  faPause,
  faPersonRunning,
  faPlay,
  faRadio,
  faStar,
  faSun,
  faTrophy,
  faTv,
  faUpRightAndDownLeftFromCenter,
  faVolleyball,
  faVolumeHigh,
  faVolumeXmark,
  faWindowMinimize,
  faXmark,
} from '@fortawesome/free-solid-svg-icons'

export const icons = {
  play: faPlay,
  circlePlay: faCirclePlay,
  download: faDownload,
  record: faCircle,
  pause: faPause,
  volume: faVolumeHigh,
  mute: faVolumeXmark,
  expand: faExpand,
  compress: faCompress,
  minimize: faWindowMinimize,
  expandPlayer: faUpRightAndDownLeftFromCenter,
  captions: faClosedCaptioning,
  audio: faHeadphones,
  menu: faEllipsisVertical,
  external: faExternalLink,
  close: faXmark,
  search: faMagnifyingGlass,
  settings: faGear,
  logout: faArrowRightFromBracket,
  film: faFilm,
  tv: faTv,
  live: faRadio,
  sports: faTrophy,
  recordings: faHardDrive,
  home: faHouse,
  moon: faMoon,
  sun: faSun,
  star: faStar,
  chevronLeft: faChevronLeft,
  chevronRight: faChevronRight,
  baseball: faBaseballBatBall,
  basketball: faBasketball,
  football: faFootball,
  hockey: faHockeyPuck,
  soccer: faFutbol,
  golf: faGolfBallTee,
  volleyball: faVolleyball,
  running: faPersonRunning,
}

/** Font Awesome icon for a sports_events.sport label. */
export function sportIcon(sport: string): IconDefinition {
  const key = sport.trim().toLowerCase()
  if (key.startsWith('external')) return icons.sports
  switch (key) {
    case 'baseball':
      return icons.baseball
    case 'basketball':
      return icons.basketball
    case 'american football':
      return icons.football
    case 'ice hockey':
    case 'hockey':
      return icons.hockey
    case 'football':
    case 'soccer':
      return icons.soccer
    case 'golf':
      return icons.golf
    case 'volleyball':
      return icons.volleyball
    case 'athletics':
    case 'track':
      return icons.running
    case 'motor sports':
    case 'fight':
    case 'tennis':
    case 'rugby':
    case 'cricket':
    case 'darts':
    case 'billiards':
    case 'afl':
      return icons.sports
    default:
      return icons.sports
  }
}

export function Icon({
  icon,
  className,
  title,
}: {
  icon: IconDefinition
  className?: string
  title?: string
}) {
  return <FontAwesomeIcon icon={icon} className={className} title={title} aria-hidden={title ? undefined : true} />
}
