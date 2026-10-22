import { useEffect, useState } from "react";
import { Download, FolderOpen, RotateCcw, X } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { SourceStatus } from "@/components/SourceStatus";
import { useDownloads } from "@/hooks/useDownloads";
import { cancelAllDownloads, cancelDownload, clearFinishedDownloads, guardContinue, refreshDownloads, retryDownload, retryFailedDownloads, type DownloadItem } from "@/lib/downloads";
import { useI18n, type MessageKey } from "@/lib/i18n";
import { getSettings } from "@/lib/settings";
import { toastWithSound as toast } from "@/lib/toast-with-sound";
import { errorMessage } from "@/lib/utils";
import { OpenFolder } from "../../wailsjs/go/main/App";

const ACTIVE = ["queued", "downloading"];
const DONE = ["completed", "skipped"];
const FAILED = ["failed", "cancelled"];
const TABS: { key: string; label: MessageKey; statuses?: string[] }[] = [
    { key: "all", label: "download.tabAll" },
    { key: "active", label: "download.tabActive", statuses: ACTIVE },
    { key: "done", label: "download.tabDone", statuses: DONE },
    { key: "failed", label: "download.tabFailed", statuses: FAILED },
];
const hhmm = (ms: number) => {
    const date = new Date(ms);
    return `${String(date.getHours()).padStart(2, "0")}:${String(date.getMinutes()).padStart(2, "0")}`;
};
const head = "h-10 px-4 text-left align-middle font-medium text-muted-foreground text-xs uppercase";

export function DownloadsPage() {
    const { t, language } = useI18n();
    const { items, activeCount, guard } = useDownloads();
    const [tab, setTab] = useState("all");
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
        {FAILED.includes(item.status) && rowAction("download.retry", RotateCcw, () => retryDownload(item.id))}
        {DONE.includes(item.status) && rowAction("download.openFolder", FolderOpen, () => OpenFolder(item.path))}
    </>);
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
                    <Button variant="outline" size="sm" disabled={!has([...DONE, ...FAILED])} className="cursor-pointer" onClick={() => run(clearFinishedDownloads)}>{t("download.clearFinished")}</Button>
                </div>
            </div>

            <div className="flex gap-2 border-b shrink-0">
                {TABS.map(({ key, label }) => (<Button key={key} variant={tab === key ? "default" : "ghost"} size="sm" onClick={() => setTab(key)} className="rounded-b-none">{t(label)}</Button>))}
            </div>
        </div>

        {guard.paused_until > 0 && (<div className="flex items-center justify-between gap-4 rounded-md border p-4 text-sm">
            <span>{t("download.pausedUntil", { time: hhmm(guard.paused_until) })}</span>
            <Button variant="outline" size="sm" className="cursor-pointer" onClick={() => run(guardContinue)}>{t("download.resumeNow")}</Button>
        </div>)}

        {getSettings().downloadSource === "doubledouble" && (<div className="rounded-md border p-4"><SourceStatus compact/></div>)}

        <div className="rounded-md border overflow-hidden">
            {shown.length === 0 ? (<div className="flex flex-col items-center justify-center py-12 text-center text-muted-foreground gap-3">
                <Download className="h-10 w-10 opacity-40"/>
                <p className="font-medium text-foreground/80">{t("download.empty")}</p>
            </div>) : (<table className="w-full table-fixed">
                <thead>
                    <tr className="border-b bg-muted/50">
                        <th className={`${head} w-12`}>#</th>
                        <th className={`${head} w-1/3`}>{t("table.title")}</th>
                        <th className={`${head} w-40`}>{t("download.colStatus")}</th>
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
                            <Badge variant={item.status === "failed" ? "destructive" : item.status === "completed" ? "default" : "secondary"}>{t(`download.status.${item.status}` as MessageKey)}</Badge>
                            {item.status === "downloading" && item.message && (<div className="mt-1 text-xs text-muted-foreground truncate" title={item.message}>{item.message}</div>)}
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
    </div>);
}
