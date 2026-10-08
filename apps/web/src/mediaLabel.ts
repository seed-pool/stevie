import { formatBytes, type MediaFile } from './api'

export function mediaVersionLabel(file: MediaFile) {
  const video = file.streams?.find((s) => s.codec_type === 'video')
  const audio = file.streams?.find((s) => s.codec_type === 'audio')
  const res = video?.height
    ? video.height >= 2160
      ? '2160p'
      : video.height >= 1080
        ? '1080p'
        : video.height >= 720
          ? '720p'
          : `${video.height}p`
    : undefined
  return [res, video?.codec_name?.toUpperCase(), audio?.codec_name?.toUpperCase(), formatBytes(file.size_bytes)]
    .filter(Boolean)
    .join(' · ')
}
