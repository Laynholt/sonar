import { useState, useEffect, useLayoutEffect, useMemo, useRef, useCallback } from "react";
import { Button } from "@/components/ui/button";
import { InputWithContext } from "@/components/ui/input-with-context";
import { XCircle, Link, Search, X, ChevronDown, ArrowLeft } from "lucide-react";
import { Spinner } from "@/components/ui/spinner";
import { Tooltip, TooltipContent, TooltipTrigger, } from "@/components/ui/tooltip";
import { FetchHistory } from "@/components/FetchHistory";
import { Search as SearchService, SearchByType } from "../../wailsjs/go/main/App";
import type { history } from "../../wailsjs/go/models";
import { model } from "../../wailsjs/go/models";
import { cn, errorMessage } from "@/lib/utils";
import { useTypingEffect } from "@/hooks/useTypingEffect";
import { t as translate, useI18n } from "@/lib/i18n";
import { SourceIcon } from "@/components/ServiceBadge";
import { linkLabels, useSources } from "@/hooks/useSources";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle, } from "@/components/ui/dialog";
const FETCH_LINKS = [
    "https://open.spotify.com/track/...",
    "https://open.spotify.com/album/...",
    "https://open.spotify.com/playlist/...",
    "https://open.spotify.com/artist/...",
    "https://music.amazon.com/albums/...",
    "https://music.amazon.com/playlists/...",
    "https://music.amazon.com/artists/..."
];
const SEARCH_PLACEHOLDERS = [
    "Golden",
    "Taylor Swift",
    "The Weeknd",
    "Starboy",
    "Joji",
    "Die For You"
];
type ResultTab = "tracks" | "albums" | "artists" | "playlists";
const RECENT_SEARCHES_KEY = "sonar_recent_searches";
const SEARCH_SERVICE_KEY = "sonar_search_service";
const MAX_RECENT_SEARCHES = 8;
const SEARCH_LIMIT = 50;
// Spotify's pages overlap and can repeat ids, so keep only ids not already seen.
const uniqueById = <T extends {
    id: string;
}>(items: T[], seen = new Set<string>()) => items.filter((item) => !seen.has(item.id) && seen.add(item.id));
interface SearchBarProps {
    url: string;
    loading: boolean;
    onUrlChange: (url: string) => void;
    onFetch: () => void;
    onFetchUrl: (url: string) => Promise<void>;
    history: history.FetchItem[];
    onHistorySelect: (item: history.FetchItem) => void;
    onHistoryRemove: (id: string) => void;
    hasResult: boolean;
    searchMode: boolean;
    onSearchModeChange: (isSearch: boolean) => void;
    canGoBack: boolean;
    onBack: () => void;
}
export function SearchBar({ url, loading, onUrlChange, onFetch, onFetchUrl, history, onHistorySelect, onHistoryRemove, hasResult, searchMode, onSearchModeChange, canGoBack, onBack, }: SearchBarProps) {
    const { t, language } = useI18n();
    const { sources, label, can } = useSources();
    const [savedService, setSavedService] = useState(() => {
        try {
            return localStorage.getItem(SEARCH_SERVICE_KEY) ?? "";
        }
        catch {
            return "";
        }
    });
    // The remembered service, else the first one that can search (e.g. when the remembered plugin is off).
    const searchable = sources.filter((s) => s.can.includes("search"));
    const searchService = (searchable.find((s) => s.service === savedService) ?? searchable[0])?.service ?? "";
    const canPage = can(searchService, "searchByType");
    const linkServices = linkLabels(sources);
    const [searchQuery, setSearchQuery] = useState("");
    const [searchResults, setSearchResults] = useState<model.SearchResponse | null>(null);
    const [searchStatus, setSearchStatus] = useState<"idle" | "debouncing" | "loading" | "success" | "empty" | "error">("idle");
    const [searchError, setSearchError] = useState("");
    const [loadMoreError, setLoadMoreError] = useState("");
    const [retry, setRetry] = useState(0);
    const [isLoadingMore, setIsLoadingMore] = useState(false);
    const [lastSearchedQuery, setLastSearchedQuery] = useState("");
    const [activeTab, setActiveTab] = useState<ResultTab>("tracks");
    const [recentSearches, setRecentSearches] = useState<string[]>([]);
    const [hasMore, setHasMore] = useState<Record<ResultTab, boolean>>({
        tracks: false,
        albums: false,
        artists: false,
        playlists: false,
    });
    const [showInvalidUrlDialog, setShowInvalidUrlDialog] = useState(false);
    const [invalidUrl, setInvalidUrl] = useState("");
    const searchGeneration = useRef(0);
    const pageGeneration = useRef(0);
    // Items received from the API per tab (before dedupe): the real position of the next page.
    const rawOffset = useRef<Record<ResultTab, number>>({ tracks: 0, albums: 0, artists: 0, playlists: 0 });
    const invalidateRequests = useCallback(() => {
        searchGeneration.current++;
        pageGeneration.current++;
    }, []);
    const isSearching = searchStatus === "loading" || searchStatus === "debouncing";
    // Stable between renders: useTypingEffect restarts whenever the array changes.
    const placeholders = useMemo(() => searchMode ? SEARCH_PLACEHOLDERS : [translate("search.paste", { service: linkServices }), ...FETCH_LINKS], [searchMode, language, linkServices]);
    const placeholderText = useTypingEffect(placeholders);
    useEffect(() => {
        try {
            const saved = localStorage.getItem(RECENT_SEARCHES_KEY);
            if (saved) {
                setRecentSearches(JSON.parse(saved));
            }
        }
        catch (error) {
            console.error("Failed to load recent searches:", error);
        }
    }, []);
    const saveRecentSearch = useCallback((query: string) => {
        const trimmed = query.trim();
        if (!trimmed)
            return;
        setRecentSearches((prev) => {
            const filtered = prev.filter((s) => s.toLowerCase() !== trimmed.toLowerCase());
            const updated = [trimmed, ...filtered].slice(0, MAX_RECENT_SEARCHES);
            try {
                localStorage.setItem(RECENT_SEARCHES_KEY, JSON.stringify(updated));
            }
            catch (error) {
                console.error("Failed to save recent searches:", error);
            }
            return updated;
        });
    }, []);
    const removeRecentSearch = (query: string) => {
        setRecentSearches((prev) => {
            const updated = prev.filter((s) => s !== query);
            try {
                localStorage.setItem(RECENT_SEARCHES_KEY, JSON.stringify(updated));
            }
            catch (error) {
                console.error("Failed to save recent searches:", error);
            }
            return updated;
        });
    };
    const invalidateSearch = () => {
        invalidateRequests();
        setSearchResults(null);
        setLastSearchedQuery("");
        setIsLoadingMore(false);
        setLoadMoreError("");
        setSearchError("");
    };
    const changeQuery = (query: string) => {
        invalidateSearch();
        setSearchStatus(query.trim() ? "debouncing" : "idle");
        setSearchQuery(query);
    };
    const changeMode = (mode: boolean) => {
        invalidateSearch();
        onSearchModeChange(mode);
    };
    const changeService = (next: string) => {
        setSavedService(next);
        try {
            localStorage.setItem(SEARCH_SERVICE_KEY, next);
        }
        catch (error) {
            console.error("Failed to save search service:", error);
        }
    };
    const changeTab = (tab: ResultTab) => {
        pageGeneration.current++;
        setIsLoadingMore(false);
        setLoadMoreError("");
        setActiveTab(tab);
    };
    useLayoutEffect(() => {
        invalidateRequests();
        const generation = searchGeneration.current;
        setSearchResults(null);
        setLastSearchedQuery("");
        setIsLoadingMore(false);
        setLoadMoreError("");
        setSearchError("");
        const query = searchQuery.trim();
        setSearchStatus(searchMode && query ? "debouncing" : "idle");
        if (!searchMode || !query || !searchService) return;
        const timeout = setTimeout(async () => {
            if (generation !== searchGeneration.current) return;
            setSearchStatus("loading");
            try {
                const raw = await SearchService(searchService, query, SEARCH_LIMIT);
                if (generation !== searchGeneration.current) return;
                rawOffset.current = { tracks: raw.tracks.length, albums: raw.albums.length, artists: raw.artists.length, playlists: raw.playlists.length };
                const results = new model.SearchResponse({
                    tracks: uniqueById(raw.tracks),
                    albums: uniqueById(raw.albums),
                    artists: uniqueById(raw.artists),
                    playlists: uniqueById(raw.playlists),
                });
                setSearchResults(results);
                setLastSearchedQuery(query);
                saveRecentSearch(query);
                setSearchStatus(results.tracks.length || results.albums.length || results.artists.length || results.playlists.length ? "success" : "empty");
                // A source that cannot search by type has no paging, so it never offers load more.
                const more = (items: unknown[]) => canPage && items.length === SEARCH_LIMIT;
                setHasMore({
                    tracks: more(raw.tracks),
                    albums: more(raw.albums),
                    artists: more(raw.artists),
                    playlists: more(raw.playlists),
                });
                if (results.tracks.length > 0)
                    setActiveTab("tracks");
                else if (results.albums.length > 0)
                    setActiveTab("albums");
                else if (results.artists.length > 0)
                    setActiveTab("artists");
                else if (results.playlists.length > 0)
                    setActiveTab("playlists");
            }
            catch (error) {
                if (generation !== searchGeneration.current) return;
                setSearchError(errorMessage(error, translate("search.failedFallback")));
                setSearchStatus("error");
            }
        }, 400);
        return () => {
            clearTimeout(timeout);
            invalidateRequests();
        };
    }, [searchQuery, searchMode, searchService, canPage, retry, saveRecentSearch, invalidateRequests]);
    const handleLoadMore = async () => {
        if (!searchResults || !lastSearchedQuery || isLoadingMore)
            return;
        const typeMap: Record<ResultTab, string> = {
            tracks: "track",
            albums: "album",
            artists: "artist",
            playlists: "playlist",
        };
        const generation = searchGeneration.current;
        const page = ++pageGeneration.current;
        const tab = activeTab;
        const isCurrent = () => generation === searchGeneration.current && page === pageGeneration.current;
        setIsLoadingMore(true);
        setLoadMoreError("");
        try {
            const moreResults = await SearchByType(searchService, lastSearchedQuery, typeMap[activeTab], SEARCH_LIMIT, rawOffset.current[tab]);
            if (!isCurrent()) return;
            rawOffset.current[tab] += moreResults.length;
            if (moreResults.length > 0) {
                setSearchResults((prev) => {
                    if (!prev || !isCurrent())
                        return prev;
                    const fresh = uniqueById(moreResults, new Set(prev[tab].map((item) => item.id)));
                    const updated = new model.SearchResponse({
                        tracks: tab === "tracks" ? [...prev.tracks, ...fresh] : prev.tracks,
                        albums: tab === "albums" ? [...prev.albums, ...fresh] : prev.albums,
                        artists: tab === "artists" ? [...prev.artists, ...fresh] : prev.artists,
                        playlists: tab === "playlists" ? [...prev.playlists, ...fresh] : prev.playlists,
                    });
                    return updated;
                });
            }
            setHasMore((prev) => ({
                ...prev,
                [tab]: moreResults.length === SEARCH_LIMIT,
            }));
        }
        catch (error) {
            if (isCurrent()) setLoadMoreError(errorMessage(error, translate("search.loadFailedFallback")));
        }
        finally {
            if (isCurrent()) setIsLoadingMore(false);
        }
    };
    const isSupportedUrl = (text: string) => {
        const trimmed = text.trim();
        if (!trimmed)
            return true;
        const isUrl = /^(https?:\/\/|www\.)/i.test(trimmed) || /^spotify:/i.test(trimmed);
        if (!isUrl)
            return true;
        return trimmed.includes("spotify.com") ||
            /^(https?:\/\/|www\.)[^/]*music\.amazon\./i.test(trimmed) ||
            trimmed.includes("spotify.link") ||
            trimmed.startsWith("spotify:");
    };
    const handlePaste = (e: React.ClipboardEvent<HTMLInputElement>) => {
        if (searchMode)
            return;
        const pastedText = e.clipboardData.getData("text");
        if (pastedText && !isSupportedUrl(pastedText)) {
            e.preventDefault();
            setInvalidUrl(pastedText);
            setShowInvalidUrlDialog(true);
        }
    };
    const handleFetchWithValidation = () => {
        if (!isSupportedUrl(url)) {
            setInvalidUrl(url);
            setShowInvalidUrlDialog(true);
            return;
        }
        onFetch();
    };
    const handleResultClick = (externalUrl: string) => {
        changeMode(false);
        onFetchUrl(externalUrl);
    };
    const formatDuration = (ms: number) => {
        const minutes = Math.floor(ms / 60000);
        const seconds = Math.floor((ms % 60000) / 1000);
        return `${minutes}:${seconds.toString().padStart(2, "0")}`;
    };
    const hasAnyResults = searchResults && (searchResults.tracks.length > 0 ||
        searchResults.albums.length > 0 ||
        searchResults.artists.length > 0 ||
        searchResults.playlists.length > 0);
    const getTabCount = (tab: ResultTab): number => {
        if (!searchResults)
            return 0;
        switch (tab) {
            case "tracks": return searchResults.tracks.length;
            case "albums": return searchResults.albums.length;
            case "artists": return searchResults.artists.length;
            case "playlists": return searchResults.playlists.length;
        }
    };
    const tabs: {
        key: ResultTab;
        label: string;
    }[] = [
        { key: "tracks", label: t("types.tracks") },
        { key: "albums", label: t("types.albums") },
        { key: "artists", label: t("types.artists") },
        { key: "playlists", label: t("types.playlists") },
    ];
    return (<div className="space-y-4">
            <div className="flex gap-2">
                <Button variant={canGoBack ? "default" : "outline"} size="icon" aria-label={t("common.back")} className="shrink-0" disabled={!canGoBack} onClick={onBack}>
                    <ArrowLeft className="h-4 w-4"/>
                </Button>
                <Tooltip>
                    <TooltipTrigger asChild>
                        <Button variant="outline" size="icon" aria-label={searchMode ? t("search.toFetch") : t("search.toSearch")} className="shrink-0" onClick={() => changeMode(!searchMode)}>
                            {searchMode ? (<Link className="h-4 w-4"/>) : (<Search className="h-4 w-4"/>)}
                        </Button>
                    </TooltipTrigger>
                    <TooltipContent>
                        <p>{searchMode ? t("search.fetchMode") : t("search.searchMode")}</p>
                    </TooltipContent>
                </Tooltip>

                <div className="relative flex-1">
                    {!searchMode ? (<>
                            <InputWithContext id="spotify-url" placeholder={placeholderText} value={url} onChange={(e) => onUrlChange(e.target.value)} onPaste={handlePaste} onKeyDown={(e) => e.key === "Enter" && handleFetchWithValidation()} className="pr-8"/>
                            {url && (<button type="button" aria-label={t("search.clear")} className="absolute right-2 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground transition-colors cursor-pointer" onClick={() => onUrlChange("")}>
                                    <XCircle className="h-4 w-4"/>
                                </button>)}
                        </>) : (<>
                            <InputWithContext id="spotify-search" aria-label={t("search.aria", { service: label(searchService) })} placeholder={placeholderText} value={searchQuery} onChange={(e) => changeQuery(e.target.value)} className="pr-8"/>
                            {searchQuery && (<button type="button" aria-label={t("search.clear")} className="absolute right-2 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground transition-colors cursor-pointer" onClick={() => changeQuery("")}>
                                    <XCircle className="h-4 w-4"/>
                                </button>)}
                        </>)}
                </div>

                {searchMode && searchable.length > 1 && (<div role="radiogroup" aria-label={t("search.service")} className="flex shrink-0 rounded-md border">
                        {searchable.map((option) => (<Button key={option.service} type="button" role="radio" aria-checked={searchService === option.service} variant={searchService === option.service ? "secondary" : "ghost"} onClick={() => changeService(option.service)}>
                                <SourceIcon icon={option.icon}/>
                                {option.label}
                            </Button>))}
                    </div>)}

                {!searchMode && (<Button onClick={handleFetchWithValidation} disabled={loading}>
                        {loading ? (<>
                                <Spinner />
                                {t("search.fetching")}
                            </>) : (<>
                                <Search className="h-4 w-4"/>
                                {t("search.fetch")}
                            </>)}
                    </Button>)}
            </div>

            {!searchMode && !hasResult && (<FetchHistory history={history} onSelect={onHistorySelect} onRemove={onHistoryRemove}/>)}

            {searchMode && (<div className="space-y-4">
                    {!searchQuery && !searchResults && recentSearches.length > 0 && (<div className="space-y-2">
                            <p className="text-sm text-muted-foreground">{t("search.recent")}</p>
                            <div className="flex flex-wrap gap-2">
                                {recentSearches.map((query) => (<div key={query} className="group relative flex items-center px-3 py-1.5 bg-muted hover:bg-accent rounded-full text-sm cursor-pointer transition-colors" onClick={() => changeQuery(query)}>
                                        <span>{query}</span>
                                        <button type="button" className="absolute -top-1.5 -right-1.5 z-10 w-5 h-5 rounded-full bg-red-500 hover:bg-red-600 flex items-center justify-center opacity-0 group-hover:opacity-100 transition-all cursor-pointer shadow-sm" onClick={(e) => {
                        e.stopPropagation();
                        removeRecentSearch(query);
                    }}>
                                            <X className="h-3 w-3 text-red-900" strokeWidth={3}/>
                                        </button>
                                    </div>))}
                            </div>
                        </div>)}

                    {isSearching && (<div className="flex items-center justify-center py-8">
                            <Spinner />
                            <span className="ml-2 text-muted-foreground" role="status">{searchStatus === "debouncing" ? t("search.waiting") : t("search.searching")}</span>
                        </div>)}

                    {searchStatus === "error" && (<div className="text-center py-8 space-y-2">
                            <p role="alert">{t("search.failed", { error: searchError })}</p>
                            <Button variant="outline" onClick={() => { invalidateSearch(); setRetry((value) => value + 1); }}>{t("search.retry")}</Button>
                        </div>)}
                    {searchStatus === "empty" && (<div className="text-center py-8 text-muted-foreground">
                            {t("search.noResults", { query: searchQuery })}
                        </div>)}

                    {!isSearching && hasAnyResults && (<>
                            <div className="flex gap-1 border-b">
                                {tabs.map((tab) => {
                    const count = getTabCount(tab.key);
                    if (count === 0)
                        return null;
                    return (<button key={tab.key} type="button" onClick={() => changeTab(tab.key)} className={cn("px-4 py-2 text-sm font-medium transition-colors cursor-pointer border-b-2 -mb-px", activeTab === tab.key
                            ? "border-primary text-foreground"
                            : "border-transparent text-muted-foreground hover:text-foreground")}>
                                            {tab.label} ({count})
                                        </button>);
                })}
                            </div>

                            <div className="grid gap-2">
                                {activeTab === "tracks" &&
                    searchResults?.tracks.map((track) => (<button key={track.id} type="button" className="flex items-center gap-3 p-3 rounded-lg bg-card hover:bg-accent border cursor-pointer text-left transition-colors" onClick={() => handleResultClick(track.external_urls)}>
                                            {track.images ? (<img src={track.images} alt="" className="w-12 h-12 rounded object-cover shrink-0"/>) : (<div className="w-12 h-12 rounded bg-muted shrink-0"/>)}
                                            <div className="flex-1 min-w-0">
                                                <div className="flex items-center gap-1.5 min-w-0">
                                                    <p className="font-medium truncate">{track.name}</p>
                                                    {track.is_explicit && (<span className="flex items-center justify-center min-w-[16px] h-[16px] rounded bg-red-600 text-[10px] font-bold text-white leading-none shrink-0" title={t("track.explicit")}>
                                                            E
                                                        </span>)}
                                                </div>
                                                <p className="text-sm text-muted-foreground truncate">
                                                    {track.artists}
                                                </p>
                                            </div>
                                            {!!track.duration_ms && (<span className="text-sm text-muted-foreground shrink-0">
                                                {formatDuration(track.duration_ms)}
                                            </span>)}
                                        </button>))}

                                {activeTab === "albums" &&
                    searchResults?.albums.map((album) => (<button key={album.id} type="button" className="flex items-center gap-3 p-3 rounded-lg bg-card hover:bg-accent border cursor-pointer text-left transition-colors" onClick={() => handleResultClick(album.external_urls)}>
                                            {album.images ? (<img src={album.images} alt="" className="w-12 h-12 rounded object-cover shrink-0"/>) : (<div className="w-12 h-12 rounded bg-muted shrink-0"/>)}
                                            <div className="flex-1 min-w-0">
                                                <p className="font-medium truncate">{album.name}</p>
                                                <p className="text-sm text-muted-foreground truncate">
                                                    {album.artists}
                                                </p>
                                            </div>
                                            <span className="text-sm text-muted-foreground shrink-0">
                                                {album.release_date || ""}
                                            </span>
                                        </button>))}

                                {activeTab === "artists" &&
                    searchResults?.artists.map((artist) => (<button key={artist.id} type="button" className="flex items-center gap-3 p-3 rounded-lg bg-card hover:bg-accent border cursor-pointer text-left transition-colors" onClick={() => handleResultClick(artist.external_urls)}>
                                            {artist.images ? (<img src={artist.images} alt="" className="w-12 h-12 rounded-full object-cover shrink-0"/>) : (<div className="w-12 h-12 rounded-full bg-muted shrink-0"/>)}
                                            <div className="flex-1 min-w-0">
                                                <p className="font-medium truncate">{artist.name}</p>
                                                <p className="text-sm text-muted-foreground">{t("type.artist")}</p>
                                            </div>
                                        </button>))}

                                {activeTab === "playlists" &&
                    searchResults?.playlists.map((playlist) => (<button key={playlist.id} type="button" className="flex items-center gap-3 p-3 rounded-lg bg-card hover:bg-accent border cursor-pointer text-left transition-colors" onClick={() => handleResultClick(playlist.external_urls)}>
                                            {playlist.images ? (<img src={playlist.images} alt="" className="w-12 h-12 rounded object-cover shrink-0"/>) : (<div className="w-12 h-12 rounded bg-muted shrink-0"/>)}
                                            <div className="flex-1 min-w-0">
                                                <p className="font-medium truncate">{playlist.name}</p>
                                                <p className="text-sm text-muted-foreground truncate">
                                                    {playlist.owner || ""}
                                                </p>
                                            </div>
                                        </button>))}
                            </div>

                            {loadMoreError && <p role="alert" className="text-center">{t("search.loadFailed", { error: loadMoreError })}</p>}
                            {hasMore[activeTab] && (<div className="flex justify-center pt-2">
                                    <Button variant="outline" onClick={handleLoadMore} disabled={isLoadingMore}>
                                        {isLoadingMore ? (<>
                                                <Spinner />
                                                {t("search.loading")}
                                            </>) : (<>
                                                <ChevronDown className="h-4 w-4"/>
                                                {loadMoreError ? t("search.retryMore") : t("search.loadMore")}
                                            </>)}
                                    </Button>
                                </div>)}
                        </>)}
                </div>)}

            <Dialog open={showInvalidUrlDialog} onOpenChange={setShowInvalidUrlDialog}>
                <DialogContent className="sm:max-w-[425px]">
                    <DialogHeader>
                        <DialogTitle>{t("search.invalidTitle")}</DialogTitle>
                        <DialogDescription>
                            {t("search.invalidBody", { service: linkServices })}
                        </DialogDescription>
                    </DialogHeader>

                    {invalidUrl && (<div className="p-3 bg-muted rounded-md border text-xs font-mono break-all opacity-70">
                            {invalidUrl}
                        </div>)}

                    <DialogFooter>
                        <Button variant="outline" onClick={() => {
            setShowInvalidUrlDialog(false);
            setInvalidUrl("");
        }}>
                            {t("common.cancel")}
                        </Button>
                        <Button onClick={() => {
            changeMode(true);
            setShowInvalidUrlDialog(false);
            setInvalidUrl("");
        }}>
                            {t("search.switchToSearch")}
                        </Button>
                    </DialogFooter>
                </DialogContent>
            </Dialog>
        </div>);
}
