import type { TrackMetadata } from "@/types/api";
export type ArtistRef = {
    id: string;
    name: string;
    external_urls: string;
};
export type AlbumRef = ArtistRef;
const linkClass = "cursor-pointer hover:underline";
export function ArtistLinks({ track, onArtistClick, className }: {
    track: TrackMetadata;
    onArtistClick?: (artist: ArtistRef) => void;
    className?: string;
}) {
    const link = (artist: ArtistRef) => onArtistClick && artist.id && artist.external_urls
        ? <button type="button" className={linkClass} onClick={() => onArtistClick(artist)}>{artist.name}</button>
        : artist.name;
    // The Go list builders send artists_data without names; those come from the "A, B" string.
    const names = track.artists.split(", ").map((name) => name.trim());
    const data = track.artists_data ?? [];
    const artists: ArtistRef[] | null = data.length
        ? data.every((artist) => artist.name)
            ? data
            : names.length === data.length ? data.map((artist, i) => ({ ...artist, name: names[i] })) : null
        : track.artist_id && track.artist_url
            ? [{ id: track.artist_id, name: track.artists, external_urls: track.artist_url }]
            : null;
    return (<span className={className}>
    {!artists ? track.artists : artists.map((artist, i) => (<span key={artist.id || i}>
      {i > 0 && ", "}
      {link(artist)}
    </span>))}
  </span>);
}
export function AlbumLink({ track, onAlbumClick }: {
    track: TrackMetadata;
    onAlbumClick?: (album: AlbumRef) => void;
}) {
    if (!onAlbumClick || !track.album_id || !track.album_url)
        return <>{track.album_name}</>;
    return (<button type="button" className={linkClass} onClick={() => onAlbumClick({ id: track.album_id!, name: track.album_name, external_urls: track.album_url! })}>
    {track.album_name}
  </button>);
}
