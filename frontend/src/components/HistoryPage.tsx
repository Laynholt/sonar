import { useCallback, useEffect, useMemo, useState } from "react";
import { Button } from "@/components/ui/button";
import { Trash2, Search, Database, Music2, Disc3, ListMusic, UserRound } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Input } from "@/components/ui/input";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Pagination, PaginationContent, PaginationEllipsis, PaginationItem, PaginationLink, PaginationNext, PaginationPrevious } from "@/components/ui/pagination";
import { GetFetchHistory, DeleteFetchHistoryItem, ClearFetchHistory, ClearFetchHistoryByType } from "../../wailsjs/go/main/App";
import type { history } from "../../wailsjs/go/models";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { toastWithSound as toast } from "@/lib/toast-with-sound";
import { errorMessage } from "@/lib/utils";
import { t as translate, useI18n, type MessageKey } from "@/lib/i18n";
import { historyInfo } from "@/lib/history-info";
const formatDate = (timestamp: number) => {
    const date = new Date(timestamp * 1000);
    const year = date.getFullYear();
    const month = String(date.getMonth() + 1).padStart(2, '0');
    const day = String(date.getDate()).padStart(2, '0');
    const hours = String(date.getHours()).padStart(2, '0');
    const minutes = String(date.getMinutes()).padStart(2, '0');
    const seconds = String(date.getSeconds()).padStart(2, '0');
    return `${year}-${month}-${day} ${hours}:${minutes}:${seconds}`;
};
interface HistoryPageProps {
    onHistorySelect: (id: string, sourceUrl: string) => void;
}
const ITEMS_PER_PAGE = 50;
const TYPE_TABS: { type: string; label: MessageKey; single: MessageKey; Icon: typeof Music2 }[] = [
    { type: "track", label: "types.tracks", single: "type.track", Icon: Music2 },
    { type: "album", label: "types.albums", single: "type.album", Icon: Disc3 },
    { type: "playlist", label: "types.playlists", single: "type.playlist", Icon: ListMusic },
    { type: "artist", label: "types.artists", single: "type.artist", Icon: UserRound },
];
export function HistoryPage({ onHistorySelect }: HistoryPageProps) {
    const { t, language } = useI18n();
    const [items, setItems] = useState<history.FetchItem[]>([]);
    const [loadError, setLoadError] = useState("");
    const [searchQuery, setSearchQuery] = useState("");
    const [currentPage, setCurrentPage] = useState(1);
    const [activeType, setActiveType] = useState("track");
    const [clearScope, setClearScope] = useState<"type" | "all" | null>(null);
    const activeTab = TYPE_TABS.find((tab) => tab.type === activeType)!;
    const reload = useCallback(async () => {
        try {
            setItems((await GetFetchHistory()) || []);
            setLoadError("");
        }
        catch (err) {
            setLoadError(errorMessage(err, translate("history.loadFailed")));
        }
    }, []);
    useEffect(() => { void reload(); }, [reload]);
    const typeCount = useMemo(() => items.filter((item) => item.type === activeType).length, [items, activeType]);
    const filtered = useMemo(() => {
        const query = searchQuery.toLowerCase();
        return items
            .filter((item) => item.type === activeType)
            .filter((item) => !query || item.name.toLowerCase().includes(query) || historyInfo(item).toLowerCase().includes(query))
            .sort((a, b) => b.timestamp - a.timestamp);
    }, [items, searchQuery, activeType, language]);
    const changeSearch = (value: string) => {
        setSearchQuery(value);
        setCurrentPage(1);
    };
    const changeType = (type: string) => {
        setActiveType(type);
        setCurrentPage(1);
    };
    const mutate = async (action: () => Promise<void>) => {
        try {
            await action();
        }
        catch (err) {
            toast.error(errorMessage(err, translate("history.updateFailed")));
        }
        await reload();
    };
    const confirmClear = async () => {
        const scope = clearScope;
        setClearScope(null);
        await mutate(() => scope === "all" ? ClearFetchHistory() : ClearFetchHistoryByType(activeType));
        setCurrentPage(1);
    };
    const getPaginationPages = (current: number, total: number): (number | 'ellipsis')[] => {
        if (total <= 10)
            return Array.from({ length: total }, (_, i) => i + 1);
        const pages: (number | 'ellipsis')[] = [];
        pages.push(1);
        if (current <= 7) {
            for (let i = 2; i <= 10; i++)
                pages.push(i);
            pages.push('ellipsis');
            pages.push(total);
        }
        else if (current >= total - 7) {
            pages.push('ellipsis');
            for (let i = total - 9; i <= total; i++)
                pages.push(i);
        }
        else {
            pages.push('ellipsis');
            pages.push(current - 1);
            pages.push(current);
            pages.push(current + 1);
            pages.push('ellipsis');
            pages.push(total);
        }
        return pages;
    };
    const totalPages = Math.ceil(filtered.length / ITEMS_PER_PAGE);
    // Deleting the last row of the last page leaves the stored page past the end.
    const activePage = Math.min(currentPage, totalPages || 1);
    const startIndex = (activePage - 1) * ITEMS_PER_PAGE;
    const paginated = filtered.slice(startIndex, startIndex + ITEMS_PER_PAGE);
    return (<div className="space-y-6">
            <h1 className="text-2xl font-bold">{t("nav.history")}</h1>

            <div className="space-y-6">
                <div className="flex flex-col gap-4">
                    <div className="flex items-center justify-between">
                        <div className="flex items-center gap-2">
                            <h2 className="text-xl font-bold tracking-tight">{t("history.fetches")}</h2>
                            {filtered.length > 0 && (<Badge variant="secondary" className="font-mono">
                                    {filtered.length.toLocaleString(language)}
                                </Badge>)}
                        </div>
                        <div className="flex items-center gap-2">
                            <Button variant="outline" size="sm" onClick={() => setClearScope("type")} disabled={typeCount === 0} className="cursor-pointer gap-2">
                                <Trash2 className="h-4 w-4"/> {t("history.clearType", { type: t(activeTab.label) })}
                            </Button>
                            <Button variant="outline" size="sm" onClick={() => setClearScope("all")} disabled={items.length === 0} className="cursor-pointer gap-2">
                                <Trash2 className="h-4 w-4"/> {t("history.clearAll")}
                            </Button>
                        </div>
                    </div>

                    <div className="flex gap-2 border-b shrink-0">
                        {TYPE_TABS.map(({ type, label, Icon }) => (<Button key={type} variant={activeType === type ? "default" : "ghost"} size="sm" onClick={() => changeType(type)} className="rounded-b-none">
                                <Icon className="h-4 w-4"/>
                                {t(label)}
                            </Button>))}
                    </div>

                    <div className="relative">
                        <Search className="absolute left-2 top-2.5 h-4 w-4 text-muted-foreground"/>
                        <Input placeholder={t("history.search")} value={searchQuery} onChange={(e) => changeSearch(e.target.value)} className="pl-8 h-9"/>
                    </div>
                </div>

                {loadError && (<p role="alert" className="rounded-md border border-destructive/50 p-4 text-sm text-destructive">{t("history.loadError", { error: loadError })}</p>)}

                <div className="rounded-md border overflow-hidden">
                    {paginated.length === 0 ? (<div className="flex flex-col items-center justify-center py-12 text-center text-muted-foreground gap-3">
                            <Database className="h-10 w-10 opacity-40"/>
                            <div className="space-y-1">
                                <p className="font-medium text-foreground/80">{t("history.empty")}</p>
                                <p className="text-sm">{t("history.emptyHint")}</p>
                            </div>
                        </div>) : (<table className="w-full table-fixed">
                            <thead>
                                <tr className="border-b bg-muted/50">
                                    <th className="h-10 px-4 text-left align-middle font-medium text-muted-foreground w-12 text-xs uppercase">#</th>
                                    <th className="h-10 px-4 text-left align-middle font-medium text-muted-foreground text-xs uppercase w-1/3">
                                        {activeType === "artist" ? t("history.colName") : t("table.title")}
                                    </th>
                                    <th className="h-10 px-4 text-left align-middle font-medium text-muted-foreground hidden md:table-cell text-xs uppercase">{t("history.colDetails")}</th>
                                    <th className="h-10 px-4 text-left align-middle font-medium text-muted-foreground hidden lg:table-cell w-40 text-xs uppercase text-nowrap">{t("history.colFetchedAt")}</th>
                                    <th className="h-10 px-4 text-center align-middle font-medium text-muted-foreground w-32 text-xs uppercase text-nowrap">{t("table.actions")}</th>
                                </tr>
                            </thead>
                            <tbody>
                                {paginated.map((item, index) => (<tr key={item.id} className="border-b transition-colors hover:bg-muted/50 cursor-pointer" onClick={() => onHistorySelect(item.id, item.url)}>
                                        <td className="p-3 align-middle text-sm text-muted-foreground text-left font-mono">
                                            {startIndex + index + 1}
                                        </td>
                                        <td className="p-3 align-middle min-w-0">
                                            <div className="flex items-center gap-3 min-w-0">
                                                <div className="h-8 w-8 rounded shrink-0 bg-secondary overflow-hidden">
                                                    {item.image ? (<img src={item.image} alt={item.name} className="h-full w-full object-cover"/>) : (<div className="h-full w-full flex items-center justify-center text-[10px] text-muted-foreground font-medium bg-muted">
                                                            {item.type.slice(0, 2).toUpperCase()}
                                                        </div>)}
                                                </div>
                                                <span className="font-medium text-sm truncate">{item.name}</span>
                                            </div>
                                        </td>
                                        <td className="p-3 align-middle text-sm text-muted-foreground hidden md:table-cell">
                                            <div className="truncate" title={historyInfo(item)}>{historyInfo(item)}</div>
                                        </td>
                                        <td className="p-3 align-middle text-xs text-muted-foreground hidden lg:table-cell whitespace-nowrap">
                                            <div className="flex flex-col">
                                                <span>{formatDate(item.timestamp).split(' ')[0]}</span>
                                                <span className="text-[10px] text-muted-foreground">{formatDate(item.timestamp).split(' ')[1]}</span>
                                            </div>
                                        </td>
                                        <td className="p-3 align-middle text-center" onClick={(e) => e.stopPropagation()}>
                                            <div className="flex items-center justify-center gap-1">
                                                <Tooltip delayDuration={0}>
                                                    <TooltipTrigger asChild>
                                                        <Button variant="ghost" size="icon" aria-label={t("history.delete")} className="h-8 w-8 cursor-pointer text-destructive hover:text-destructive" onClick={() => void mutate(() => DeleteFetchHistoryItem(item.id))}>
                                                            <Trash2 className="h-4 w-4"/>
                                                        </Button>
                                                    </TooltipTrigger>
                                                    <TooltipContent>
                                                        <p>{t("history.delete")}</p>
                                                    </TooltipContent>
                                                </Tooltip>
                                            </div>
                                        </td>
                                    </tr>))}
                            </tbody>
                        </table>)}
                </div>

                 {totalPages > 1 && (<Pagination>
                        <PaginationContent>
                            <PaginationItem>
                                <PaginationPrevious href="#" onClick={(e) => {
                    e.preventDefault();
                    if (activePage > 1)
                        setCurrentPage(activePage - 1);
                }} className={activePage === 1 ? "pointer-events-none opacity-50" : "cursor-pointer"}/>
                            </PaginationItem>
                            
                            {getPaginationPages(activePage, totalPages).map((page, index) => (page === 'ellipsis' ? (<PaginationItem key={`ellipsis-${index}`}>
                                        <PaginationEllipsis />
                                    </PaginationItem>) : (<PaginationItem key={page}>
                                        <PaginationLink href="#" onClick={(e) => {
                        e.preventDefault();
                        setCurrentPage(page as number);
                    }} isActive={activePage === page} className="cursor-pointer">
                                            {page}
                                        </PaginationLink>
                                    </PaginationItem>)))}

                            <PaginationItem>
                                <PaginationNext href="#" onClick={(e) => {
                    e.preventDefault();
                    if (activePage < totalPages)
                        setCurrentPage(activePage + 1);
                }} className={activePage === totalPages ? "pointer-events-none opacity-50" : "cursor-pointer"}/>
                            </PaginationItem>
                        </PaginationContent>
                    </Pagination>)}
            </div>

            <Dialog open={clearScope !== null} onOpenChange={(open) => !open && setClearScope(null)}>
                <DialogContent className="max-w-md [&>button]:hidden">
                    <DialogHeader>
                        <DialogTitle>{clearScope === "all" ? t("history.clearAllTitle") : t("history.clearTypeTitle", { type: t(activeTab.label) })}</DialogTitle>
                        <DialogDescription>
                            {clearScope === "all" ? t("history.clearAllBody") : t("history.clearTypeBody", { type: t(activeTab.single).toLowerCase() })} {t("history.irreversible")}
                        </DialogDescription>
                    </DialogHeader>
                    <DialogFooter>
                        <Button variant="outline" onClick={() => setClearScope(null)} className="cursor-pointer">{t("common.cancel")}</Button>
                        <Button variant="destructive" onClick={() => void confirmClear()} className="cursor-pointer">
                            {t("history.clearConfirm")}
                        </Button>
                    </DialogFooter>
                </DialogContent>
            </Dialog>
        </div>);
}
