declare module 'mpegts.js' {
  export interface Config {
    enableWorker?: boolean
    enableWorkerForMSE?: boolean
    enableStashBuffer?: boolean
    stashInitialSize?: number
    isLive?: boolean
    liveBufferLatencyChasing?: boolean
    liveBufferLatencyChasingOnPaused?: boolean
    liveBufferLatencyMaxLatency?: number
    liveBufferLatencyMinRemain?: number
    liveSync?: boolean
    liveSyncMaxLatency?: number
    liveSyncTargetLatency?: number
    liveSyncPlaybackRate?: number
    lazyLoad?: boolean
    lazyLoadMaxDuration?: number
    lazyLoadRecoverDuration?: number
    deferLoadAfterSourceOpen?: boolean
    autoCleanupSourceBuffer?: boolean
    autoCleanupMaxBackwardDuration?: number
    autoCleanupMinBackwardDuration?: number
    fixAudioTimestampGap?: boolean
  }

  export interface Player {
    attachMediaElement(el: HTMLMediaElement): void
    load(): void
    play(): Promise<void> | void
    pause(): void
    unload(): void
    detachMediaElement(): void
    destroy(): void
    on(event: string, listener: (...args: unknown[]) => void): void
  }

  export interface MPEGTSStatic {
    isSupported(): boolean
    getFeatureList(): { mseLivePlayback?: boolean }
    createPlayer(
      mediaDataSource: { type: string; isLive?: boolean; url: string },
      config?: Config,
    ): Player
    Events: { ERROR: string }
  }

  const mpegts: MPEGTSStatic
  export default mpegts
}
