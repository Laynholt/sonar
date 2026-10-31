import { useRef, useState } from "react";
import { fetchMetadata } from "@/lib/api";
import { toastWithSound as toast } from "@/lib/toast-with-sound";
import { logger } from "@/lib/logger";
import { errorMessage } from "@/lib/utils";
import { t } from "@/lib/i18n";
import { linkLabels, useSources } from "@/hooks/useSources";
import { extractArtistID } from "@/lib/spotify-url";
import { AddFetchHistory } from "../../wailsjs/go/main/App";
import type { MetadataResponse } from "@/types/api";
import { emptyNav, navigate, type NavAction, type NavEntry, type NavState, type View } from "@/hooks/navigation";

export function useMetadata() {
    const { sources } = useSources();
    const [loading, setLoading] = useState(false);
    const [nav, setNav] = useState<NavState>(emptyNav);
    const dispatch = (action: NavAction) => setNav(current => navigate(current, action));
    const generationRef = useRef(0);
    // Bumped once a fetch is stored, so the recent-fetches list can reload after the write lands.
    const [historyVersion, setHistoryVersion] = useState(0);
    const loadedMetadata = nav.current;
    const metadata = loadedMetadata?.data ?? null;
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
                info = String(data.track_list.length);
                image = data.album_info.images;
            }
            else if ("playlist_info" in data) {
                type = "playlist";
                name = data.playlist_info.owner.name;
                info = String(data.playlist_info.tracks.total);
                image = data.playlist_info.cover || "";
            }
            else if ("artist_info" in data) {
                type = "artist";
                name = data.artist_info.name;
                info = String(data.artist_info.total_albums || data.album_list.length);
                image = data.artist_info.images;
            }
            const jsonStr = JSON.stringify(data);
            // Every payload carries the canonical link; a pasted spotify: URI, ?si= or name-slug form must not persist,
            // or one page gets two history entries.
            const canonicalUrl = ("track" in data && data.track.external_urls) || ("album_info" in data && data.album_info.external_urls) || ("playlist_info" in data && data.playlist_info.external_urls) || ("artist_info" in data && data.artist_info.external_urls) || url;
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
    const fetchMetadataDirectly = async (url: string, generation: number, view?: View): Promise<boolean> => {
        const urlType = getUrlType(url);
        logger.info(`fetching ${urlType} metadata...`);
        logger.debug(`url: ${url}`);
        setLoading(true);
        try {
            const startTime = Date.now();
            const data = await fetchMetadata(url);
            if (!isCurrentGeneration(generation)) return false;
            if (!isKnownMetadataResponse(data)) {
                logger.error(`unknown metadata payload for url: ${url}`);
                toast.error(t("meta.parseFailed"));
                return false;
            }
            const elapsed = ((Date.now() - startTime) / 1000).toFixed(2);
            if ("playlist_info" in data) {
                const playlistInfo = data.playlist_info;
                if (!playlistInfo.owner.name && playlistInfo.tracks.total === 0 && data.track_list.length === 0) {
                    logger.warning("playlist appears to be empty or private");
                    toast.error(t("meta.playlistMissing"));
                    return false;
                }
            }
            else if ("album_info" in data) {
                const albumInfo = data.album_info;
                if (!albumInfo.name && albumInfo.total_tracks === 0 && data.track_list.length === 0) {
                    logger.warning("album appears to be empty or not found");
                    toast.error(t("meta.albumMissing"));
                    return false;
                }
            }
            dispatch(view ? { type: "push", entry: { data, sourceUrl: url }, view } : { type: "open", entry: { data, sourceUrl: url } });
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
            toast.success(t("meta.fetched"));
            return true;
        }
        catch (err) {
            if (!isCurrentGeneration(generation)) return false;
            const errorMsg = errorMessage(err, t("meta.fetchFailed"));
            logger.error(`fetch failed: ${errorMsg}`);
            toast.error(errorMsg);
            return false;
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
            dispatch({ type: "open", entry: { data, sourceUrl } });
            toast.success(t("meta.cacheLoaded"));
        }
        catch (err) {
            console.error("Failed to load from cache:", err);
            toast.error(t("meta.cacheFailed"));
        }
    };
    const handleFetchMetadata = async (url: string, view?: View) => {
        const generation = beginGeneration();
        if (!url.trim()) {
            setLoading(false);
            logger.warning("empty url provided");
            toast.error(t("meta.enterUrl", { service: linkLabels(sources) }));
            return;
        }
        let urlToFetch = url.trim();
        const isArtistUrl = urlToFetch.includes("/artist/") || urlToFetch.startsWith("spotify:artist:");
        if (isArtistUrl) {
            const artistID = extractArtistID(urlToFetch);
            if (!artistID) {
                setLoading(false);
                logger.error(`invalid artist url: ${urlToFetch}`);
                toast.error(t("meta.invalidArtist"));
                return;
            }
            urlToFetch = `https://open.spotify.com/artist/${artistID}/discography/all`;
            logger.debug("normalized artist url to discography");
        }
        if (isArtistUrl) logger.info("artist url detected");
        const fetched = await fetchMetadataDirectly(urlToFetch, generation, view);
        return fetched && isCurrentGeneration(generation) ? urlToFetch : undefined;
    };
    const goBack = (): NavEntry | null => {
        const previous = nav.back[nav.back.length - 1] ?? null;
        if (!previous) return null;
        beginGeneration();
        setLoading(false);
        dispatch({ type: "back" });
        return previous;
    };
    return {
        loading,
        metadata,
        loadedMetadata,
        historyVersion,
        canGoBack: nav.back.length > 0 && !loading,
        handleFetchMetadata,
        loadFromCache,
        goBack,
        resetMetadata: () => {
            beginGeneration();
            dispatch({ type: "close" });
            setLoading(false);
        },
    };
}
