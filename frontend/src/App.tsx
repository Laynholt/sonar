import { useState, useEffect, useLayoutEffect, useCallback, useRef } from "react";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle, } from "@/components/ui/dialog";
import { TooltipProvider } from "@/components/ui/tooltip";
import { getSettings, getSettingsLoadError, loadSettings, applyThemeMode, applyFont } from "@/lib/settings";
import { applyTheme } from "@/lib/themes";
import { toastWithSound as toast } from "@/lib/toast-with-sound";
import { TitleBar } from "@/components/TitleBar";
import { Sidebar, type PageType } from "@/components/Sidebar";
import { Header } from "@/components/Header";
import { SettingsPage } from "@/components/SettingsPage";
import { DebugLoggerPage } from "@/components/DebugLoggerPage";
import { AboutPage } from "@/components/AboutPage";
import { HistoryPage } from "@/components/HistoryPage";
import { Search } from "lucide-react";
import { SearchBar } from "@/components/SearchBar";
import { TrackInfo } from "@/components/TrackInfo";
import { AlbumInfo } from "@/components/AlbumInfo";
import { PlaylistInfo } from "@/components/PlaylistInfo";
import { ArtistInfo } from "@/components/ArtistInfo";
import { useMetadata } from "@/hooks/useMetadata";
import { extractArtistID } from "@/lib/spotify-url";
import { errorMessage } from "@/lib/utils";
import { DeleteFetchHistoryItem, GetFetchHistory } from "../wailsjs/go/main/App";
import type { history } from "../wailsjs/go/models";
import type { TrackMetadata } from "@/types/api";

const ITEMS_PER_PAGE = 50;
const RECENT_FETCHES = 5;

function applySavedAppearance() {
    const settings = getSettings();
    applyThemeMode(settings.themeMode);
    applyTheme(settings.theme);
    applyFont(settings.fontFamily);
}

function App() {
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
                toast.error("Settings file could not be read; using the last local copy", { description: String(loadError) });
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
            toast.error("Could not load recent fetches", { description: errorMessage(err, "Unknown error") });
        }
    }, []);
    // Refreshes on mount, after each stored fetch, and when returning from the History page.
    useEffect(() => { if (currentPage === "main") void loadRecentFetches(); }, [currentPage, metadata.historyVersion, loadRecentFetches]);
    const removeRecentFetch = async (id: string) => {
        try {
            await DeleteFetchHistoryItem(id);
        }
        catch (err) {
            toast.error(errorMessage(err, "Failed to delete history entry"));
        }
        await loadRecentFetches();
    };
    const openCached = (data: string, sourceUrl: string) => {
        setSpotifyUrl(sourceUrl);
        setIsSearchMode(false);
        metadata.loadFromCache(data, sourceUrl);
        setCurrentPage("main");
    };
    const loaded = metadata.loadedMetadata;
    useEffect(() => {
        setSearchQuery("");
        setSortBy("default");
        setTrackPage(1);
    }, [loaded]);
    const fetchUrl = async (url: string) => {
        setSpotifyUrl(url);
        const fetched = await metadata.handleFetchMetadata(url);
        if (fetched) setSpotifyUrl(fetched);
    };
    const openArtist = (artist: { id: string; external_urls: string }) => {
        if (loaded?.data.service !== "spotify") {
            // Stage 2 wires the other services; follow the artist's own link if there is one.
            if (artist.external_urls) void fetchUrl(artist.external_urls);
            return;
        }
        const artistID = extractArtistID(artist.id) || extractArtistID(artist.external_urls || "");
        if (!artistID) {
            toast.error("Failed to open artist");
            return;
        }
        void fetchUrl(`https://open.spotify.com/artist/${artistID}/discography/all`);
    };
    const openTrack = (track: TrackMetadata) => {
        if (track.external_urls) void fetchUrl(track.external_urls);
    };
    const renderMetadata = () => {
        if (!loaded) return null;
        const { data, sourceUrl } = loaded;
        const list = {
            searchQuery, sortBy, currentPage: trackPage, itemsPerPage: ITEMS_PER_PAGE,
            onSearchChange: (value: string) => { setSearchQuery(value); setTrackPage(1); }, onSortChange: setSortBy, onPageChange: setTrackPage,
            onTrackClick: openTrack, onArtistClick: openArtist, onBack: metadata.resetMetadata,
        };
        if ("track" in data) return <TrackInfo track={data.track} onBack={metadata.resetMetadata}/>;
        if ("album_info" in data) return <AlbumInfo service={data.service} sourceUrl={sourceUrl} albumInfo={data.album_info} trackList={data.track_list} {...list}/>;
        if ("playlist_info" in data) return <PlaylistInfo service={data.service} sourceUrl={sourceUrl} playlistInfo={data.playlist_info} trackList={data.track_list} onAlbumClick={metadata.handleAlbumClick} {...list}/>;
        return <ArtistInfo service={data.service} artistInfo={data.artist_info} albumList={data.album_list} trackList={data.track_list} onAlbumClick={metadata.handleAlbumClick} {...list}/>;
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
                return <HistoryPage onHistorySelect={openCached}/>;
            default:
                return (<>
                    <Header />
                    <SearchBar url={spotifyUrl} loading={metadata.loading} onUrlChange={setSpotifyUrl} onFetch={() => fetchUrl(spotifyUrl)} onFetchUrl={fetchUrl} history={recentFetches} onHistorySelect={(item) => openCached(item.data, item.url)} onHistoryRemove={(id) => void removeRecentFetch(id)} hasResult={!!loaded} searchMode={isSearchMode} onSearchModeChange={setIsSearchMode}/>
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

            <Dialog open={metadata.showAlbumDialog} onOpenChange={metadata.setShowAlbumDialog}>
                <DialogContent className="sm:max-w-[425px]">
                    <DialogHeader>
                        <DialogTitle>Fetch Album</DialogTitle>
                        <DialogDescription>Do you want to fetch metadata for this album?</DialogDescription>
                    </DialogHeader>
                    {metadata.selectedAlbum && (<p className="font-medium bg-muted/50 rounded-md px-3 py-2">{metadata.selectedAlbum.name}</p>)}
                    <DialogFooter>
                        <Button variant="outline" onClick={() => metadata.setShowAlbumDialog(false)}>
                            Cancel
                        </Button>
                        <Button onClick={async () => {
                            const albumUrl = await metadata.handleConfirmAlbumFetch();
                            if (albumUrl) setSpotifyUrl(albumUrl);
                        }}>
                            <Search className="h-4 w-4"/>
                            Fetch Album
                        </Button>
                    </DialogFooter>
                </DialogContent>
            </Dialog>

            <Dialog open={showUnsavedChangesDialog} onOpenChange={setShowUnsavedChangesDialog}>
                <DialogContent className="sm:max-w-[425px] [&>button]:hidden">
                    <DialogHeader>
                        <DialogTitle>Unsaved Changes</DialogTitle>
                        <DialogDescription>
                            You have unsaved changes in Settings. Are you sure you want to leave? Your changes will be lost.
                        </DialogDescription>
                    </DialogHeader>
                    <DialogFooter>
                        <Button variant="outline" onClick={handleCancelNavigation}>
                            Cancel
                        </Button>
                        <Button variant="destructive" onClick={handleDiscardChanges}>
                            Discard Changes
                        </Button>
                    </DialogFooter>
                </DialogContent>
            </Dialog>
        </div>
    </TooltipProvider>);
}
export default App;
