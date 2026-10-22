import { CircleCheck, Download, Loader2, RotateCcw } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import type { BatchProgress, DownloadItem } from "@/lib/downloads";
import { useI18n } from "@/lib/i18n";
import type { TrackMetadata } from "@/types/api";

export interface DownloadRowProps {
    canDownload?: boolean;
    downloadItems?: Map<string, DownloadItem>;
    onDownloadTrack?: (track: TrackMetadata, indexInFullList: number) => void;
    onRetryDownload?: (id: string) => void;
}
export interface DownloadCardProps extends DownloadRowProps {
    onDownloadAll?: () => void;
    downloadProgress?: BatchProgress;
    onStopDownload?: () => void;
}

interface DownloadButtonProps {
    item?: DownloadItem;
    disabled?: boolean;
    disabledReason?: string;
    onDownload: () => void;
    onRetry: () => void;
}

// The wrapper span carries the tooltip because a disabled button receives no pointer events.
function WithTip({ tip, children }: { tip: string; children: React.ReactNode }) {
    return (<Tooltip>
      <TooltipTrigger asChild>
        <span className="inline-flex" tabIndex={0} aria-description={tip}>{children}</span>
      </TooltipTrigger>
      <TooltipContent><p>{tip}</p></TooltipContent>
    </Tooltip>);
}

export function DownloadButton({ item, disabled, disabledReason, onDownload, onRetry }: DownloadButtonProps) {
    const { t } = useI18n();
    const status = item?.status;
    if (disabled) {
        return (<WithTip tip={disabledReason ?? t("download.track")}>
          <Button size="icon" variant="outline" disabled aria-label={t("download.track")}><Download className="h-4 w-4"/></Button>
        </WithTip>);
    }
    if (status === "queued" || status === "downloading") {
        const tip = item!.message || t("download.queued");
        return (<WithTip tip={tip}>
          <Button size="icon" variant="outline" disabled aria-label={tip}><Loader2 className="h-4 w-4 animate-spin"/></Button>
        </WithTip>);
    }
    if (status === "completed" || status === "skipped") {
        const tip = status === "completed" ? item!.path : t("download.exists");
        return (<WithTip tip={tip}>
          <CircleCheck className={`m-2 h-4 w-4 ${status === "completed" ? "text-green-500" : "text-muted-foreground"}`}/>
        </WithTip>);
    }
    if (status === "failed") {
        return (<WithTip tip={item!.error}>
          <Button size="icon" variant="ghost" aria-label={item!.error} onClick={onRetry}><RotateCcw className="h-4 w-4 text-red-500"/></Button>
        </WithTip>);
    }
    return (<WithTip tip={t("download.track")}>
      <Button size="icon" variant="outline" aria-label={t("download.track")} onClick={onDownload}><Download className="h-4 w-4"/></Button>
    </WithTip>);
}

// Card controls: the primary button, and while the batch runs a progress line with Stop.
export function DownloadAll({ label, canDownload, onDownload, progress, onStop, size }: { label: "download.all" | "download.track"; canDownload?: boolean; onDownload?: () => void; progress?: BatchProgress; onStop?: () => void; size?: "sm" }) {
    const { t } = useI18n();
    if (!onDownload) return null;
    const button = (<Button onClick={onDownload} disabled={!canDownload} size={size}><Download className="h-4 w-4"/>{t(label)}</Button>);
    return (<>
      {canDownload ? button : <WithTip tip={t("download.spotifyLater")}>{button}</WithTip>}
      {progress?.active && (<>
        <span className="text-sm text-muted-foreground">{t("download.progress", { done: progress.done, total: progress.total, failed: progress.failed })}</span>
        {onStop && <Button variant="outline" size={size} onClick={onStop}>{t("download.stop")}</Button>}
      </>)}
    </>);
}
