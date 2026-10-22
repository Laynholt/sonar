import { useEffect, useState } from "react";
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Spinner } from "@/components/ui/spinner";
import { mb } from "@/lib/download-format";
import type { DownloadItem } from "@/lib/downloads";
import { useI18n, type MessageKey } from "@/lib/i18n";
import { errorMessage } from "@/lib/utils";
import { GetDownloadTags } from "../../wailsjs/go/main/App";
import type { main } from "../../wailsjs/go/models";

/** The tags and cover read back from a downloaded file. */
export function TagsDialog({ item, onClose }: { item: DownloadItem | null; onClose: () => void }) {
    const { t, language } = useI18n();
    const [tags, setTags] = useState<main.TrackTags | null>(null);
    const [error, setError] = useState("");
    useEffect(() => {
        setTags(null);
        setError("");
        if (!item) return;
        let live = true;
        GetDownloadTags(item.id).then((got) => live && setTags(got), (err) => live && setError(errorMessage(err, t("recent.unknownError"))));
        return () => { live = false; };
    }, [item, t]);
    const row = (label: string, value: string) => (<tr key={label} className="border-b last:border-0 align-top">
        <td className="py-1.5 pr-4 text-muted-foreground whitespace-nowrap">{label}</td>
        <td className="py-1.5 whitespace-pre-wrap break-words select-text">{value}</td>
      </tr>);
    return (<Dialog open={item !== null} onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>{t("tags.heading")}</DialogTitle>
          <DialogDescription className="truncate">{item && `${item.track.title} — ${item.track.artist}`}</DialogDescription>
        </DialogHeader>
        {error ? (<p className="text-sm text-destructive">{error}</p>) : !tags ? (<div className="flex justify-center py-8"><Spinner/></div>) : (<div className="flex gap-4 min-h-0">
            <div className="w-40 shrink-0 space-y-2">
              {tags.cover ? (<img src={tags.cover} alt={t("tags.cover")} className="w-40 h-40 rounded-md object-cover border"/>) : (<div className="w-40 h-40 rounded-md border bg-muted flex items-center justify-center text-xs text-muted-foreground text-center p-2">{t("tags.noCover")}</div>)}
              <p className="text-xs text-muted-foreground tabular-nums">{tags.format} · {t("widget.size", { size: mb(tags.size, language) })}</p>
            </div>
            <div className="flex-1 min-w-0 max-h-[60vh] overflow-y-auto pr-3 text-sm">
              <table className="w-full">
                <tbody>
                  {tags.fields?.map((f) => row(t(`tags.${f.key}` as MessageKey), f.value))}
                  {tags.extra?.map((f) => row(f.key, f.value))}
                  {row(t("tags.file"), tags.path)}
                </tbody>
              </table>
            </div>
          </div>)}
      </DialogContent>
    </Dialog>);
}
