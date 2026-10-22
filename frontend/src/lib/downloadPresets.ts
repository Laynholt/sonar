export const FOLDER_PRESETS = [
    "", "{artist}", "{album}", "[{year}] {album}", "[{year}] {artist} - {album}", "{artist}/{album}", "{artist}/[{year}] {album}", "{artist}/{year}/{album}",
    "{album_artist}", "{album_artist}/{album}", "{album_artist}/[{year}] {album}", "{album_artist}/{year}/{album}", "{year}", "{year}/{artist}",
] as const;

export const FILENAME_PRESETS = [
    "{title}", "{title} - {artist}", "{artist} - {title}", "{track}. {title}", "{track}. {title} - {artist}", "{track}. {artist} - {title}",
    "{title} - {album_artist}", "{track}. {title} - {album_artist}", "{artist} - {album} - {title}", "{track} - {title}", "{disc}-{track}. {title}", "{disc}-{track}. {title} - {artist}",
] as const;
