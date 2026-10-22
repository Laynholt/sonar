import { useEffect, useState } from "react";
import { Download, FolderOpen, RotateCcw, Tags, X } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { Progress } from "@/components/ui/progress";
import { SiteRows } from "@/components/SiteRows";
import { SourceStatus } from "@/components/SourceStatus";
import { TagsDialog } from "@/components/TagsDialog";
import { useDownloadSpeed } from "@/hooks/useDownloadSpeed";
import { useDownloads } from "@/hooks/useDownloads";
import { eta, mb } from "@/lib/download-format";
import { cancelAllDownloads, cancelDownload, clearFinishedDownloads, refreshDownloads, resumeQueue, retryDownload, retryFailedDownloads, type DownloadItem } from "@/lib/downloads";
import { useI18n, type MessageKey } from "@/lib/i18n";
import { openPicker } from "@/lib/picker";
import { getSettings } from "@/lib/settings";
import { toastWithSound as toast } from "@/lib/toast-with-sound";
import { errorMessage } from "@/lib/utils";
import { OpenFolder } from "../../wailsjs/go/main/App";

const ACTIVE = ["matching", "queued", "downloading"];
const DONE = ["completed", "skipped"];
// Tracks the user passed on (or that Amazon never had) are finished too, but have no file.
const FINISHED = [...DONE, "ignored", "not_found"];
const FAILED = ["failed", "cancelled"];
const TABS: { key: string; label: MessageKey; statuses?: string[] }[] = [
    { key: "all", label: "download.tabAll" },
    { key: "active", label: "download.tabActive", statuses: ACTIVE },
    { key: "review", label: "download.tabReview", statuses: ["review"] },
    { key: "done", label: "download.tabDone", statuses: FINISHED },
    { key: "failed", label: "download.tabFailed", statuses: FAILED },
];
const head = "h-10 px-4 text-left align-middle font-medium text-muted-foreground text-xs uppercase";

export function DownloadsPage() {
    const { t, language } = useI18n();
    const { items, activeCount, sites, held } = useDownloads();
    const speed = useDownloadSpeed(items);
    const [tab, setTab] = useState("all");
    const [tagsFor, setTagsFor] = useState<DownloadItem | null>(null);
    useEffect(() => { void refreshDownloads(); }, []);
    const statuses = TABS.find((x) => x.key === tab)!.statuses;
    const shown = statuses ? items.filter((i) => statuses.includes(i.status)) : items;
    const has = (list: string[]) => items.some((i) => list.includes(i.status));
    const run = (action: () => Promise<unknown>) => void action().catch((err) => toast.error(errorMessage(err, t("recent.unknownError"))));
    const rowAction = (label: MessageKey, Icon: typeof X, action: () => Promise<unknown>) => (<Tooltip delayDuration={0}>
        <TooltipTrigger asChild>
            <Button variant="ghost" size="icon" aria-label={t(label)} className="h-8 w-8 cursor-pointer" onClick={() => run(action)}>
                <Icon className="h-4 w-4"/>
            </Button>
        </TooltipTrigger>
        <TooltipContent><p>{t(label)}</p></TooltipContent>
    </Tooltip>);
    const actions = (item: DownloadItem) => (<>
        {ACTIVE.includes(item.status) && rowAction("download.stop", X, () => cancelDownload(item.id))}
        {item.status === "review" && (<Button variant="outline" size="sm" className="cursor-pointer" onClick={() => openPicker(item.id)}>{t("download.choose")}</Button>)}
        {FAILED.includes(item.status) && rowAction("download.retry", RotateCcw, () => retryDownload(item.id))}
        {DONE.includes(item.status) && item.path && rowAction("tags.open", Tags, async () => setTagsFor(item))}
        {DONE.includes(item.status) && rowAction("download.openFolder", FolderOpen, () => OpenFolder(item.path))}
    </>);
    const count = (list: string[]) => items.filter((i) => list.includes(i.status)).length;
    const partial = items.reduce((sum, i) => sum + (i.status === "downloading" && i.size ? i.bytes / i.size : 0), 0);
    const reviewCount = count(["review"]), doneCount = count(FINISHED), failedCount = count(FAILED), queuedCount = count(["queued"]);
    const totalBytes = items.reduce((sum, i) => sum + (i.bytes ?? 0), 0);
    // Size, speed and time left for a running item; only the size once it is done.
    const progressLine = (item: DownloadItem) => {
        const s = speed.byId.get(item.id) ?? 0;
        const size = item.size ? t("widget.sizeOf", { done: mb(item.bytes, language), size: mb(item.size, language) }) : t("widget.size", { size: mb(item.bytes, language) });
        if (item.status !== "downloading") return size;
        const left = item.size ? eta(item.size - item.bytes, s) : "";
        return [size, s > 0 && t("widget.speed", { speed: mb(s, language) }), left && t("widget.left", { time: left })].filter(Boolean).join(" · ");
    };
    const details = (item: DownloadItem) => (DONE.includes(item.status) ? item.path : FAILED.includes(item.status) ? item.error : "");
    return (<div className="space-y-6">
        <h1 className="text-2xl font-bold">{t("nav.downloads")}</h1>

        <div className="flex flex-col gap-4">
            <div className="flex items-center justify-between">
                <div className="flex items-center gap-2">
                    <h2 className="text-xl font-bold tracking-tight">{t("download.items")}</h2>
                    {items.length > 0 && (<Badge variant="secondary" className="font-mono">{items.length.toLocaleString(language)}</Badge>)}
                </div>
                <div className="flex items-center gap-2">
                    <Button variant="outline" size="sm" disabled={!has(["failed"])} className="cursor-pointer" onClick={() => run(retryFailedDownloads)}>{t("download.retryFailed")}</Button>
                    <Button variant="outline" size="sm" disabled={activeCount === 0} className="cursor-pointer" onClick={() => run(cancelAllDownloads)}>{t("download.stopAll")}</Button>
                    <Button variant="outline" size="sm" disabled={!has([...FINISHED, ...FAILED])} className="cursor-pointer" onClick={() => run(clearFinishedDownloads)}>{t("download.clearFinished")}</Button>
                </div>
            </div>

            <div className="flex gap-2 border-b shrink-0">
                {TABS.map(({ key, label }) => (<Button key={key} variant={tab === key ? "default" : "ghost"} size="sm" onClick={() => setTab(key)} className="rounded-b-none">
                    {t(label)}
                    {key === "review" && reviewCount > 0 && <> <Badge variant="secondary" className="font-mono">{reviewCount}</Badge></>}
                </Button>))}
            </div>
        </div>

        {held && (<div className="flex items-center justify-between gap-4 rounded-md border p-4 text-sm">
            <span>{t("download.held")}</span>
            <Button variant="outline" size="sm" className="cursor-pointer" onClick={() => run(resumeQueue)}>{t("download.resume")}</Button>
        </div>)}

        <SiteRows sites={sites} items={items}/>

        {items.length > 0 && (<div className="rounded-md border p-4 space-y-3 text-sm">
            <div className="flex items-baseline gap-3">
                <span className="font-medium flex-1">{t(activeCount > 0 ? "widget.progress" : "widget.finished", { done: doneCount, total: items.length })}</span>
                <span className="text-xs text-muted-foreground tabular-nums">{Math.round(((doneCount + failedCount + partial) / items.length) * 100)}%</span>
            </div>
            <Progress value={(doneCount + failedCount + partial) / items.length} className="h-2"/>
            <div className="flex flex-wrap gap-x-4 gap-y-1 text-xs text-muted-foreground tabular-nums">
                {activeCount > 0 && <span>{t("widget.speed", { speed: mb(speed.total, language) })}</span>}
                <span>{t("widget.downloaded", { size: mb(totalBytes, language) })}</span>
                {queuedCount > 0 && <span>{t("widget.waiting", { count: queuedCount })}</span>}
                {failedCount > 0 && <span className="text-destructive">{t("widget.failed", { count: failedCount })}</span>}
            </div>
        </div>)}

        {getSettings().downloadSource === "doubledouble" && (<div className="rounded-md border p-4"><SourceStatus compact/></div>)}

        <div className="rounded-md border overflow-hidden">
            {shown.length === 0 ? (<div className="flex flex-col items-center justify-center py-12 text-center text-muted-foreground gap-3">
                <Download className="h-10 w-10 opacity-40"/>
                <p className="font-medium text-foreground/80">{t(items.length > 0 ? "download.emptyTab" : "download.empty")}</p>
            </div>) : (<table className="w-full table-fixed">
                <thead>
                    <tr className="border-b bg-muted/50">
                        <th className={`${head} w-12`}>#</th>
                        <th className={`${head} w-1/3`}>{t("table.title")}</th>
                        <th className={`${head} w-64`}>{t("download.colStatus")}</th>
                        <th className={`${head} hidden md:table-cell`}>{t("history.colDetails")}</th>
                        <th className={`${head} text-center w-28 text-nowrap`}>{t("table.actions")}</th>
                    </tr>
                </thead>
                <tbody>
                    {shown.map((item, index) => (<tr key={item.id} className="border-b transition-colors hover:bg-muted/50">
                        <td className="p-3 align-middle text-sm text-muted-foreground font-mono">{index + 1}</td>
                        <td className="p-3 align-middle min-w-0">
                            <div className="font-medium text-sm truncate">{item.track.title}</div>
                            <div className="text-xs text-muted-foreground truncate">{item.track.artist}</div>
                        </td>
                        <td className="p-3 align-middle">
                            <Badge variant={item.status === "failed" ? "destructive" : item.status === "completed" ? "default" : "secondary"} className={item.status === "review" ? "bg-amber-500/15 text-amber-500" : undefined}>{t(`download.status.${item.status}` as MessageKey)}</Badge>
                            {item.status === "downloading" && item.message && (<div className="mt-1 text-xs text-muted-foreground truncate" title={item.message}>{item.message}</div>)}
                            {item.status === "downloading" && item.size > 0 && <Progress value={item.bytes / item.size} className="mt-1.5"/>}
                            {item.bytes > 0 && (item.status === "downloading" || DONE.includes(item.status)) && (<div className="mt-1 text-xs text-muted-foreground tabular-nums truncate">{progressLine(item)}</div>)}
                        </td>
                        <td className="p-3 align-middle text-sm text-muted-foreground hidden md:table-cell">
                            <div className="truncate" title={details(item)}>{details(item)}</div>
                        </td>
                        <td className="p-3 align-middle text-center">
                            <div className="flex items-center justify-center gap-1">{actions(item)}</div>
                        </td>
                    </tr>))}
                </tbody>
            </table>)}
        </div>
        <TagsDialog item={tagsFor} onClose={() => setTagsFor(null)}/>
    </div>);
}
