package store

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

type Library struct {
	ID        uuid.UUID `json:"id"`
	Name      string    `json:"name"`
	RootPath  string    `json:"root_path"`
	Type      string    `json:"type"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type MediaFile struct {
	ID            uuid.UUID       `json:"id"`
	LibraryID     uuid.UUID       `json:"library_id"`
	Path          string          `json:"path"`
	SizeBytes     int64           `json:"size_bytes"`
	Mtime         *time.Time      `json:"mtime,omitempty"`
	Inode         *int64          `json:"inode,omitempty"`
	Container     *string         `json:"container,omitempty"`
	DurationMS    *int64          `json:"duration_ms,omitempty"`
	Bitrate       *int64          `json:"bitrate,omitempty"`
	FormatName    *string         `json:"format_name,omitempty"`
	FormatTags    json.RawMessage `json:"format_tags"`
	ProbeJSON     json.RawMessage `json:"probe_json,omitempty"`
	ParsedTitle   *string         `json:"parsed_title,omitempty"`
	ParsedYear    *int            `json:"parsed_year,omitempty"`
	ParsedSeason  *int            `json:"parsed_season,omitempty"`
	ParsedEpisode *int            `json:"parsed_episode,omitempty"`
	SoftDeletedAt *time.Time      `json:"soft_deleted_at,omitempty"`
	CreatedAt     time.Time       `json:"created_at"`
	UpdatedAt     time.Time       `json:"updated_at"`
	Streams       []MediaStream   `json:"streams,omitempty"`
	// Derived at read time from probe JSON / filename.
	DolbyVision bool     `json:"dolby_vision"`
	HDR10       bool     `json:"hdr10"`
	HDR10Plus   bool     `json:"hdr10_plus"`
	HLG         bool     `json:"hlg"`
	HDRLabels   []string `json:"hdr_labels,omitempty"`
}

type MediaStream struct {
	ID                         uuid.UUID       `json:"id"`
	MediaFileID                uuid.UUID       `json:"media_file_id"`
	StreamIndex                int             `json:"stream_index"`
	CodecType                  string          `json:"codec_type"`
	CodecName                  *string         `json:"codec_name,omitempty"`
	Profile                    *string         `json:"profile,omitempty"`
	Width                      *int            `json:"width,omitempty"`
	Height                     *int            `json:"height,omitempty"`
	PixFmt                     *string         `json:"pix_fmt,omitempty"`
	FPS                        *string         `json:"fps,omitempty"`
	BitRate                    *int64          `json:"bit_rate,omitempty"`
	Channels                   *int            `json:"channels,omitempty"`
	ChannelLayout              *string         `json:"channel_layout,omitempty"`
	Language                   *string         `json:"language,omitempty"`
	Title                      *string         `json:"title,omitempty"`
	DispositionDefault         bool            `json:"disposition_default"`
	DispositionForced          bool            `json:"disposition_forced"`
	DispositionHearingImpaired bool            `json:"disposition_hearing_impaired"`
	ColorRange                 *string         `json:"color_range,omitempty"`
	ColorSpace                 *string         `json:"color_space,omitempty"`
	ColorTransfer              *string         `json:"color_transfer,omitempty"`
	BitDepth                   *int            `json:"bit_depth,omitempty"`
	Raw                        json.RawMessage `json:"raw,omitempty"`
}

type Movie struct {
	ID             uuid.UUID       `json:"id"`
	TMDBID         int             `json:"tmdb_id"`
	Title          string          `json:"title"`
	OriginalTitle  *string         `json:"original_title,omitempty"`
	Tagline        *string         `json:"tagline,omitempty"`
	Overview       *string         `json:"overview,omitempty"`
	ReleaseDate    *string         `json:"release_date,omitempty"`
	RuntimeMinutes *int            `json:"runtime_minutes,omitempty"`
	VoteAverage    *float64        `json:"vote_average,omitempty"`
	VoteCount      *int            `json:"vote_count,omitempty"`
	Popularity     *float64        `json:"popularity,omitempty"`
	PosterPath     *string         `json:"poster_path,omitempty"`
	BackdropPath   *string         `json:"backdrop_path,omitempty"`
	Genres         json.RawMessage `json:"genres"`
	CastCrew       json.RawMessage `json:"cast_crew"`
	ExternalIDs    json.RawMessage `json:"external_ids"`
	CreatedAt      time.Time       `json:"created_at"`
	UpdatedAt      time.Time       `json:"updated_at"`
	// Enriched list fields
	MediaFileID   *uuid.UUID `json:"media_file_id,omitempty"`
	Resolution    *string    `json:"resolution,omitempty"`
	VideoCodec    *string    `json:"video_codec,omitempty"`
	AudioCodec    *string    `json:"audio_codec,omitempty"`
	Container     *string    `json:"container,omitempty"`
	ReleaseCount  int        `json:"release_count,omitempty"`
}

type Show struct {
	ID            uuid.UUID       `json:"id"`
	TMDBID        int             `json:"tmdb_id"`
	Name          string          `json:"name"`
	OriginalName  *string         `json:"original_name,omitempty"`
	Tagline       *string         `json:"tagline,omitempty"`
	Overview      *string         `json:"overview,omitempty"`
	FirstAirDate  *string         `json:"first_air_date,omitempty"`
	VoteAverage   *float64        `json:"vote_average,omitempty"`
	VoteCount     *int            `json:"vote_count,omitempty"`
	Popularity    *float64        `json:"popularity,omitempty"`
	PosterPath    *string         `json:"poster_path,omitempty"`
	BackdropPath  *string         `json:"backdrop_path,omitempty"`
	Genres        json.RawMessage `json:"genres"`
	CastCrew      json.RawMessage `json:"cast_crew"`
	ExternalIDs   json.RawMessage `json:"external_ids"`
	CreatedAt     time.Time       `json:"created_at"`
	UpdatedAt     time.Time       `json:"updated_at"`
	EpisodeCount  int             `json:"episode_count,omitempty"`
}

type Episode struct {
	ID             uuid.UUID `json:"id"`
	ShowID         uuid.UUID `json:"show_id"`
	SeasonNumber   int       `json:"season_number"`
	EpisodeNumber  int       `json:"episode_number"`
	Name           *string   `json:"name,omitempty"`
	Overview       *string   `json:"overview,omitempty"`
	StillPath      *string   `json:"still_path,omitempty"`
	AirDate        *string   `json:"air_date,omitempty"`
	RuntimeMinutes *int      `json:"runtime_minutes,omitempty"`
	VoteAverage    *float64  `json:"vote_average,omitempty"`
	MediaFileID   *uuid.UUID `json:"media_file_id,omitempty"`
	Resolution    *string    `json:"resolution,omitempty"`
	VideoCodec    *string    `json:"video_codec,omitempty"`
	AudioCodec    *string    `json:"audio_codec,omitempty"`
	Container     *string    `json:"container,omitempty"`
	ReleaseCount  int        `json:"release_count,omitempty"`
}

type User struct {
	ID           uuid.UUID `json:"id"`
	Username     string    `json:"username"`
	PasswordHash string    `json:"-"`
	IsAdmin      bool      `json:"is_admin"`
}
