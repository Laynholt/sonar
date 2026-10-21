import { useRef, useState } from "react";
import { fetchMetadata } from "@/lib/api";
import { toastWithSound as toast } from "@/lib/toast-with-sound";
import { logger } from "@/lib/logger";
import { errorMessage } from "@/lib/utils";
import { extractArtistID } from "@/lib/spotify-url";
import { AddFetchHistory } from "../../wailsjs/go/main/App";
import type { LoadedMetadata, MetadataResponse } from "@/types/api";

export function loadedMetadataAfterAttempt(
    current: LoadedMetadata | null,
    sourceUrl: string,
    data?: MetadataResponse,
): LoadedMetadata | null {
    return data ? { data, sourceUrl } : current;
}

export function useMetadata() {
    const [loading, setLoading] = useState(false);
    const [loadedMetadata, setLoadedMetadata] = useState<LoadedMetadata | null>(null);
    const generationRef = useRef(0);
    // Bumped once a fetch is stored, so the recent-fetches list can reload after the write lands.
    const [historyVersion, setHistoryVersion] = useState(0);
    const metadata = loadedMetadata?.data ?? null;
    const [showAlbumDialog, setShowAlbumDialog] = useState(false);
    const [selectedAlbum, setSelectedAlbum] = useState<{
        id: string;
        name: string;
        external_urls: string;
    } | null>(null);
    const getUrlType = (url: string): string => {
        if (url.includes("/track/"))
            return "track";
        if (url.includes("/album/"))
            return "album";
        if (url.includes("/playlist/"))
            return "playlist";
        if (url.includes("/artist/"))
            return "artist";
        return "unknown";
    };
    const isKnownMetadataResponse = (data: MetadataResponse): boolean => {
        return ("track" in data) ||
            ("album_info" in data) ||
            ("playlist_info" in data) ||
            ("artist_info" in data);
    };
    const saveToHistory = async (url: string, data: MetadataResponse) => {
        try {
            let name = "";
            let info = "";
            let image = "";
            let type = "unknown";
            if ("track" in data) {
                type = "track";
                name = data.track.name;
                info = data.track.artists;
                image = (data.track.images && data.track.images.length > 0) ? data.track.images : "";
            }
            else if ("album_info" in data) {
                type = "album";
                name = data.album_info.name;
                info = `${data.track_list.length} tracks`;
                image = data.album_info.images;
            }
            else if ("playlist_info" in data) {
                type = "playlist";
                name = data.playlist_info.owner.name;
                info = `${data.playlist_info.tracks.total} tracks`;
                image = data.playlist_info.cover || "";
            }
            else if ("artist_info" in data) {
                type = "artist";
                name = data.artist_info.name;
                info = `${data.artist_info.total_albums || data.album_list.length} albums`;
                image = data.artist_info.images;
            }
            const jsonStr = JSON.stringify(data);
            // Album/playlist payloads carry the canonical link; a pasted spotify: URI or ?si= form must not persist.
            const canonicalUrl = ("album_info" in data && data.album_info.external_urls) || ("playlist_info" in data && data.playlist_info.external_urls) || url;
            await AddFetchHistory({
                id: crypto.randomUUID(),
                url: canonicalUrl,
                type: type,
                name: name,
                info: info,
                image: image,
                data: jsonStr,
                timestamp: Math.floor(Date.now() / 1000)
            });
            setHistoryVersion(version => version + 1);
        }
        catch (err) {
            console.error("Failed to save fetch history:", err);
        }
    };
    const beginGeneration = () => ++generationRef.current;
    const isCurrentGeneration = (generation: number) => generation === generationRef.current;
    const fetchMetadataDirectly = async (url: string, generation: number) => {
        const urlType = getUrlType(url);
        logger.info(`fetching ${urlType} metadata...`);
        logger.debug(`url: ${url}`);
        setLoading(true);
        try {
            const startTime = Date.now();
            const data = await fetchMetadata(url);
            if (!isCurrentGeneration(generation)) return;
            if (!isKnownMetadataResponse(data)) {
                logger.error(`unknown metadata payload for url: ${url}`);
                toast.error("Failed to parse metadata response");
                return;
            }
            const elapsed = ((Date.now() - startTime) / 1000).toFixed(2);
            if ("playlist_info" in data) {
                const playlistInfo = data.playlist_info;
                if (!playlistInfo.owner.name && playlistInfo.tracks.total === 0 && data.track_list.length === 0) {
                    logger.warning("playlist appears to be empty or private");
                    toast.error("Playlist not found or may be private");
                    return;
                }
            }
            else if ("album_info" in data) {
                const albumInfo = data.album_info;
                if (!albumInfo.name && albumInfo.total_tracks === 0 && data.track_list.length === 0) {
                    logger.warning("album appears to be empty or not found");
                    toast.error("Album not found or may be private");
                    return;
                }
            }
            setLoadedMetadata(current => loadedMetadataAfterAttempt(current, url, data));
            saveToHistory(url, data);
            if ("track" in data) {
                logger.success(`fetched track: ${data.track.name} - ${data.track.artists}`);
                logger.debug(`duration: ${data.track.duration_ms}ms`);
            }
            else if ("album_info" in data) {
                logger.success(`fetched album: ${data.album_info.name}`);
                logger.debug(`${data.track_list.length} tracks, released: ${data.album_info.release_date}`);
            }
            else if ("playlist_info" in data) {
                logger.success(`fetched playlist: ${data.track_list.length} tracks`);
                logger.debug(`by ${data.playlist_info.owner.display_name || data.playlist_info.owner.name}`);
            }
            else if ("artist_info" in data) {
                logger.success(`fetched artist: ${data.artist_info.name}`);
                logger.debug(`${data.album_list.length} albums, ${data.track_list.length} tracks`);
            }
            logger.info(`fetch completed in ${elapsed}s`);
            toast.success("Metadata fetched successfully");
        }
        catch (err) {
            if (!isCurrentGeneration(generation)) return;
            const errorMsg = errorMessage(err, "Failed to fetch metadata");
            logger.error(`fetch failed: ${errorMsg}`);
            toast.error(errorMsg);
        }
        finally {
            if (isCurrentGeneration(generation)) {
                setLoading(false);
            }
        }
    };
    const loadFromCache = (cachedData: string, sourceUrl: string) => {
        beginGeneration();
        setLoading(false);
        try {
            const data = JSON.parse(cachedData) as MetadataResponse;
            setLoadedMetadata(current => loadedMetadataAfterAttempt(current, sourceUrl, data));
            toast.success("Loaded from cache");
        }
        catch (err) {
            console.error("Failed to load from cache:", err);
            toast.error("Failed to load from cache");
        }
    };
    const handleFetchMetadata = async (url: string) => {
        const generation = beginGeneration();
        if (!url.trim()) {
            setLoading(false);
            logger.warning("empty url provided");
            toast.error("Please enter a Spotify URL");
            return;
        }
        let urlToFetch = url.trim();
        const isArtistUrl = urlToFetch.includes("/artist/") || urlToFetch.startsWith("spotify:artist:");
        if (isArtistUrl) {
            const artistID = extractArtistID(urlToFetch);
            if (!artistID) {
                setLoading(false);
                logger.error(`invalid artist url: ${urlToFetch}`);
                toast.error("Invalid artist URL");
                return;
            }
            urlToFetch = `https://open.spotify.com/artist/${artistID}/discography/all`;
            logger.debug("normalized artist url to discography");
        }
        if (isArtistUrl) {
            logger.info("artist url detected");
            await fetchMetadataDirectly(urlToFetch, generation);
        }
        else {
            await fetchMetadataDirectly(urlToFetch, generation);
        }
        return isCurrentGeneration(generation) ? urlToFetch : undefined;
    };
    const handleAlbumClick = (album: {
        id: string;
        name: string;
        external_urls: string;
    }) => {
        beginGeneration();
        setLoading(false);
        logger.debug(`album clicked: ${album.name}`);
        setSelectedAlbum(album);
        setShowAlbumDialog(true);
    };
    const handleConfirmAlbumFetch = async () => {
        if (!selectedAlbum)
            return;
        const albumUrl = selectedAlbum.external_urls;
        const generation = beginGeneration();
        logger.info(`fetching album: ${selectedAlbum.name}...`);
        logger.debug(`url: ${albumUrl}`);
        setShowAlbumDialog(false);
        setLoading(true);
        try {
            const startTime = Date.now();
            const data = await fetchMetadata(albumUrl);
            if (!isCurrentGeneration(generation)) return;
            const elapsed = ((Date.now() - startTime) / 1000).toFixed(2);
            if ("album_info" in data) {
                const albumInfo = data.album_info;
                if (!albumInfo.name && albumInfo.total_tracks === 0 && data.track_list.length === 0) {
                    logger.warning("album appears to be empty or not found");
                    toast.error("Album not found or may be private");
                    setSelectedAlbum(null);
                    return albumUrl;
                }
            }
            setLoadedMetadata(current => loadedMetadataAfterAttempt(current, albumUrl, data));
            saveToHistory(albumUrl, data);
            if ("album_info" in data) {
                logger.success(`fetched album: ${data.album_info.name}`);
                logger.debug(`${data.track_list.length} tracks, released: ${data.album_info.release_date}`);
            }
            logger.info(`fetch completed in ${elapsed}s`);
            toast.success("Album metadata fetched successfully");
            return albumUrl;
        }
        catch (err) {
            if (!isCurrentGeneration(generation)) return;
            const errorMsg = errorMessage(err, "Failed to fetch album metadata");
            logger.error(`fetch failed: ${errorMsg}`);
            toast.error(errorMsg);
        }
        finally {
            if (isCurrentGeneration(generation)) {
                setLoading(false);
                setSelectedAlbum(null);
            }
        }
    };
    return {
        loading,
        metadata,
        loadedMetadata,
        historyVersion,
        showAlbumDialog,
        setShowAlbumDialog,
        selectedAlbum,
        handleFetchMetadata,
        handleAlbumClick,
        handleConfirmAlbumFetch,
        loadFromCache,
        resetMetadata: () => {
            beginGeneration();
            setLoadedMetadata(null);
            setLoading(false);
        },
    };
}
