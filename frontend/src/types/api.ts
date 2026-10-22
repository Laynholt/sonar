export type Service = "spotify" | "amazon";
export interface ArtistSimple {
    id: string;
    name: string;
    external_urls: string;
}
export interface TrackMetadata {
    service: Service;
    id?: string;
    artists: string;
    name: string;
    album_name: string;
    album_artist?: string;
    duration_ms: number;
    images: string;
    release_date: string;
    track_number: number;
    total_tracks?: number;
    total_discs?: number;
    disc_number?: number;
    external_urls: string;
    copyright?: string;
    publisher?: string;
    plays?: string;
    is_explicit?: boolean;
    album_type?: string;
    album_id?: string;
    album_url?: string;
    artist_id?: string;
    artist_url?: string;
    artists_data?: ArtistSimple[];
    status?: string;
}
export interface TrackResponse {
    service: Service;
    track: TrackMetadata;
}
export interface AlbumInfo {
    total_tracks: number;
    name: string;
    release_date: string;
    artists: string;
    images: string;
    batch?: string;
    artist_id?: string;
    artist_url?: string;
    external_urls?: string; // canonical link; absent in history cached before it existed
}
export interface AlbumResponse {
    service: Service;
    album_info: AlbumInfo;
    track_list: TrackMetadata[];
}
export interface PlaylistInfo {
    tracks: {
        total: number;
    };
    followers: {
        total: number;
    };
    owner: {
        display_name: string;
        name: string;
        images: string;
    };
    cover?: string;
    description?: string;
    batch?: string;
    external_urls?: string;
}
export interface PlaylistResponse {
    service: Service;
    playlist_info: PlaylistInfo;
    track_list: TrackMetadata[];
}
export interface ArtistInfo {
    name: string;
    followers: number;
    images: string;
    header?: string;
    gallery?: string[];
    external_urls: string;
    discography_type: string;
    total_albums: number;
    biography?: string;
    verified?: boolean;
    listeners?: number;
    rank?: number;
    batch?: string;
}
export interface DiscographyAlbum {
    id: string;
    name: string;
    album_type: string;
    release_date: string;
    total_tracks: number;
    artists: string;
    images: string;
    external_urls: string;
}
export interface ArtistDiscographyResponse {
    service: Service;
    artist_info: ArtistInfo;
    album_list: DiscographyAlbum[];
    track_list: TrackMetadata[];
    top_tracks: TrackMetadata[]; // always an array; the backend sends [] when there are none
}
export type MetadataResponse = TrackResponse | AlbumResponse | PlaylistResponse | ArtistDiscographyResponse;
export interface LoadedMetadata {
    data: MetadataResponse;
    sourceUrl: string;
}
