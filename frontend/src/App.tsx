import { useState, useEffect, useLayoutEffect, useCallback, useRef } from "react";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle, } from "@/components/ui/dialog";
import { TooltipProvider } from "@/components/ui/tooltip";
import { getSettings, getSettingsLoadError, loadSettings, applyThemeMode, applyFont } from "@/lib/settings";
import { setLanguage, t, useI18n } from "@/lib/i18n";
import { applyTheme } from "@/lib/themes";
import { toastWithSound as toast } from "@/lib/toast-with-sound";
import { TitleBar } from "@/components/TitleBar";
import { Sidebar, type PageType } from "@/components/Sidebar";
import { Header } from "@/components/Header";
import { SettingsPage } from "@/components/SettingsPage";
import { DebugLoggerPage } from "@/components/DebugLoggerPage";
import { AboutPage } from "@/components/AboutPage";
import { HistoryPage } from "@/components/HistoryPage";
import { SearchBar } from "@/components/SearchBar";
import { TrackInfo } from "@/components/TrackInfo";
import { AlbumInfo } from "@/components/AlbumInfo";
import { PlaylistInfo } from "@/components/PlaylistInfo";
import { ArtistInfo } from "@/components/ArtistInfo";
import { useMetadata } from "@/hooks/useMetadata";
import type { View } from "@/hooks/navigation";
import { stopPreview } from "@/hooks/usePreview";
import type { AlbumRef } from "@/components/ArtistLinks";
import { extractArtistID } from "@/lib/spotify-url";
import { errorMessage } from "@/lib/utils";
import { DeleteFetchHistoryItem, GetFetchHistory, GetFetchHistoryData } from "../wailsjs/go/main/App";
import type { history } from "../wailsjs/go/models";
import type { TrackMetadata } from "@/types/api";

const ITEMS_PER_PAGE = 50;
const RECENT_FETCHES = 5;

function applySavedAppearance() {
    const settings = getSettings();
    applyThemeMode(settings.themeMode);
    applyTheme(settings.theme);
    applyFont(settings.fontFamily);
    setLanguage(settings.language);
}

function App() {
    useI18n();
    const [currentPage, setCurrentPage] = useState<PageType>("main");
    const [hasUnsavedSettings, setHasUnsavedSettings] = useState(false);
    const [pendingPageChange, setPendingPageChange] = useState<PageType | null>(null);
    const [showUnsavedChangesDialog, setShowUnsavedChangesDialog] = useState(false);
    const [spotifyUrl, setSpotifyUrl] = useState("");
    const [isSearchMode, setIsSearchMode] = useState(false);
    const [recentFetches, setRecentFetches] = useState<history.FetchItem[]>([]);
    const [searchQuery, setSearchQuery] = useState("");
    const [sortBy, setSortBy] = useState("default");
    const [trackPage, setTrackPage] = useState(1);
    const metadata = useMetadata();
    const resetSettingsFn = useRef<(() => void) | null>(null);
    const rememberSettingsReset = useCallback((reset: () => void) => { resetSettingsFn.current = reset; }, []);
    useLayoutEffect(applySavedAppearance, []);
    useEffect(() => {
        void loadSettings().then(() => {
            applySavedAppearance();
            const loadError = getSettingsLoadError();
            if (loadError) {
                toast.error(t("settings.readError"), { description: String(loadError) });
            }
        });
        const mediaQuery = window.matchMedia("(prefers-color-scheme: dark)");
        const handleChange = () => {
            if (getSettings().themeMode === "auto") applySavedAppearance();
        };
        mediaQuery.addEventListener("change", handleChange);
        return () => mediaQuery.removeEventListener("change", handleChange);
    }, []);
    const handlePageChange = (page: PageType) => {
        if (currentPage === "settings" && hasUnsavedSettings && page !== "settings") {
            setPendingPageChange(page);
            setShowUnsavedChangesDialog(true);
            return;
        }
        setCurrentPage(page);
    };
    const handleDiscardChanges = () => {
        setShowUnsavedChangesDialog(false);
        resetSettingsFn.current?.();
        applySavedAppearance();
        if (pendingPageChange) {
            setCurrentPage(pendingPageChange);
            setPendingPageChange(null);
        }
    };
    const handleCancelNavigation = () => {
        setShowUnsavedChangesDialog(false);
        setPendingPageChange(null);
    };
    const loadRecentFetches = useCallback(async () => {
        try {
            setRecentFetches(((await GetFetchHistory()) || []).slice(0, RECENT_FETCHES));
        }
        catch (err) {
            toast.error(t("recent.loadFailed"), { description: errorMessage(err, t("recent.unknownError")) });
        }
    }, []);
    // Refreshes on mount, after each stored fetch, and when returning from the History page.
    useEffect(() => { if (currentPage === "main") void loadRecentFetches(); }, [currentPage, metadata.historyVersion, loadRecentFetches]);
    const removeRecentFetch = async (id: string) => {
        try {
            await DeleteFetchHistoryItem(id);
        }
        catch (err) {
            toast.error(errorMessage(err, t("history.deleteFailed")));
        }
        await loadRecentFetches();
    };
    const openCached = (data: string, sourceUrl: string) => {
        setSpotifyUrl(sourceUrl);
        setIsSearchMode(false);
        metadata.loadFromCache(data, sourceUrl);
        setCurrentPage("main");
    };
    // The history list carries no payloads; load the one being opened.
    const openHistoryItem = async (id: string, sourceUrl: string) => {
        try {
            openCached(await GetFetchHistoryData(id), sourceUrl);
        }
        catch (err) {
            toast.error(errorMessage(err, t("history.openFailed")));
        }
    };
    const loaded = metadata.loadedMetadata;
    // A fresh card starts on defaults; one reached with Back brings its saved view.
    useEffect(() => {
        setSearchQuery(loaded?.view?.searchQuery ?? "");
        setSortBy(loaded?.view?.sortBy ?? "default");
        setTrackPage(loaded?.view?.page ?? 1);
        stopPreview();
    }, [loaded]);
    useEffect(() => { if (currentPage !== "main") stopPreview(); }, [currentPage]);
    // Without a view the fetch opens a new chain; with one it is a click inside the current card.
    const fetchUrl = async (url: string, view?: View) => {
        if (!view) setSpotifyUrl(url);
        const fetched = await metadata.handleFetchMetadata(url, view);
        if (fetched) setSpotifyUrl(fetched);
    };
    const chainFetch = (url: string) => fetchUrl(url, { searchQuery, sortBy, page: trackPage });
    const goBack = () => {
        const previous = metadata.goBack();
        if (!previous) return;
        setSpotifyUrl(previous.sourceUrl);
        setIsSearchMode(false);
    };
    // Alt+Left and the mouse back button, only on the main page.
    useEffect(() => {
        if (currentPage !== "main") return;
        const onKeyDown = (event: KeyboardEvent) => {
            if (event.altKey && event.key === "ArrowLeft" && metadata.canGoBack) { event.preventDefault(); goBack(); }
        };
        const onMouseUp = (event: MouseEvent) => {
            if (event.button === 3 && metadata.canGoBack) { event.preventDefault(); goBack(); }
        };
        window.addEventListener("keydown", onKeyDown);
        window.addEventListener("mouseup", onMouseUp);
        return () => { window.removeEventListener("keydown", onKeyDown); window.removeEventListener("mouseup", onMouseUp); };
    });
    const openArtist = (artist: { id: string; external_urls: string }) => {
        if (loaded?.data.service !== "spotify") {
            // Stage 2 wires the other services; follow the artist's own link if there is one.
            if (artist.external_urls) void chainFetch(artist.external_urls);
            return;
        }
        const artistID = extractArtistID(artist.id) || extractArtistID(artist.external_urls || "");
        if (!artistID) {
            toast.error(t("artist.openFailed"));
            return;
        }
        void chainFetch(`https://open.spotify.com/artist/${artistID}/discography/all`);
    };
    const openTrack = (track: TrackMetadata) => {
        if (track.external_urls) void chainFetch(track.external_urls);
    };
    const openAlbum = (album: AlbumRef) => {
        if (album.external_urls) void chainFetch(album.external_urls);
    };
    const renderMetadata = () => {
        if (!loaded) return null;
        const { data, sourceUrl } = loaded;
        const list = {
            searchQuery, sortBy, currentPage: trackPage, itemsPerPage: ITEMS_PER_PAGE,
            onSearchChange: (value: string) => { setSearchQuery(value); setTrackPage(1); }, onSortChange: setSortBy, onPageChange: setTrackPage,
            onTrackClick: openTrack, onArtistClick: openArtist, onBack: metadata.resetMetadata,
        };
        if ("track" in data) return <TrackInfo track={data.track} onBack={metadata.resetMetadata} onArtistClick={openArtist} onAlbumClick={openAlbum}/>;
        if ("album_info" in data) return <AlbumInfo service={data.service} sourceUrl={data.album_info.external_urls || sourceUrl} albumInfo={data.album_info} trackList={data.track_list} {...list}/>;
        if ("playlist_info" in data) return <PlaylistInfo service={data.service} sourceUrl={data.playlist_info.external_urls || sourceUrl} playlistInfo={data.playlist_info} trackList={data.track_list} onAlbumClick={openAlbum} {...list}/>;
        return <ArtistInfo service={data.service} artistInfo={data.artist_info} albumList={data.album_list} trackList={data.track_list} onAlbumClick={openAlbum} {...list}/>;
    };
    const renderPage = () => {
        switch (currentPage) {
            case "settings":
                return <SettingsPage onUnsavedChangesChange={setHasUnsavedSettings} onResetRequest={rememberSettingsReset}/>;
            case "debug":
                return <DebugLoggerPage />;
            case "about":
                return <AboutPage />;
            case "history":
                return <HistoryPage onHistorySelect={openHistoryItem}/>;
            default:
                return (<>
                    <Header />
                    <SearchBar url={spotifyUrl} loading={metadata.loading} onUrlChange={setSpotifyUrl} onFetch={() => fetchUrl(spotifyUrl)} onFetchUrl={(url) => fetchUrl(url)} history={recentFetches} onHistorySelect={(item) => void openHistoryItem(item.id, item.url)} onHistoryRemove={(id) => void removeRecentFetch(id)} hasResult={!!loaded} searchMode={isSearchMode} onSearchModeChange={setIsSearchMode} canGoBack={metadata.canGoBack} onBack={goBack}/>
                    {!isSearchMode && renderMetadata()}
                </>);
        }
    };
    return (<TooltipProvider>
        <div className="min-h-screen bg-background flex flex-col">
            <TitleBar />
            <Sidebar currentPage={currentPage} onPageChange={handlePageChange}/>

            <div className="flex-1 ml-14 mt-10 p-4 md:p-8">
                <div className={`mx-auto space-y-6 ${currentPage === "main" ? "w-full" : "max-w-4xl"}`}>
                    {renderPage()}
                </div>
            </div>

            <Dialog open={showUnsavedChangesDialog} onOpenChange={setShowUnsavedChangesDialog}>
                <DialogContent className="sm:max-w-[425px] [&>button]:hidden">
                    <DialogHeader>
                        <DialogTitle>{t("unsaved.title")}</DialogTitle>
                        <DialogDescription>
                            {t("unsaved.body")}
                        </DialogDescription>
                    </DialogHeader>
                    <DialogFooter>
                        <Button variant="outline" onClick={handleCancelNavigation}>
                            {t("common.cancel")}
                        </Button>
                        <Button variant="destructive" onClick={handleDiscardChanges}>
                            {t("unsaved.discard")}
                        </Button>
                    </DialogFooter>
                </DialogContent>
            </Dialog>
        </div>
    </TooltipProvider>);
}
export default App;
