import { useEffect, useState, type ReactNode } from "react";
import { Music, Pause, Play } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Spinner } from "@/components/ui/spinner";
import { useDownloads } from "@/hooks/useDownloads";
import { usePreview } from "@/hooks/usePreview";
import { useSources } from "@/hooks/useSources";
import { clock } from "@/lib/download-format";
import { chooseCandidate, skipReview, useLink as submitLink, type DownloadItem } from "@/lib/downloads";
import { useI18n } from "@/lib/i18n";
import { toastWithSound as toast } from "@/lib/toast-with-sound";
import { cn, errorMessage } from "@/lib/utils";

const MAX_CANDIDATES = 4;

/** The review item after `id` in queue order, wrapping to the first; `id` itself is never returned. */
const nextReview = (all: DownloadItem[], id: string) => {
    const at = all.findIndex((i) => i.id === id);
    const others = all.filter((i) => i.status === "review" && i.id !== id);
    return others.find((i) => all.indexOf(i) > at) ?? others[0];
};

const meta = (...parts: (string | undefined)[]) => parts.filter(Boolean).join(" · ");

function PlayButton({ service, id, name }: { service: string; id: string; name: string }) {
    const { t } = useI18n();
    const { playPreview, loadingPreview, playingTrack } = usePreview();
    const { can } = useSources();
    if (!can(service, "preview")) return null;
    const loading = loadingPreview === id, playing = playingTrack === id;
    return (<button type="button" disabled={loading} aria-label={t(playing ? "preview.stop" : "preview.play")} onClick={() => void playPreview(service, id, name)}
        className="flex h-7 w-7 shrink-0 cursor-pointer items-center justify-center rounded-full border hover:bg-accent disabled:cursor-default">
      {loading ? <Spinner className="h-3 w-3"/> : playing ? <Pause className="h-3 w-3"/> : <Play className="h-3 w-3"/>}
    </button>);
}

interface TrackRowProps { service: string; id: string; title: string; sub: string; image: string; ms: number; className?: string; badge?: ReactNode; children?: ReactNode }

function TrackRow({ service, id, title, sub, image, ms, className, badge, children }: TrackRowProps) {
    return (<div className={cn("flex items-center gap-3 px-3 py-2", className)}>
      {id && <PlayButton service={service} id={id} name={title}/>}
      {image ? <img src={image} alt="" className="h-11 w-11 shrink-0 rounded-md object-cover"/> : <div className="flex h-11 w-11 shrink-0 items-center justify-center rounded-md bg-muted text-muted-foreground"><Music className="h-5 w-5"/></div>}
      <div className="min-w-0 flex-1">
        <div className="flex items-center gap-2"><span className="truncate font-medium text-sm" title={title}>{title}</span>{badge}</div>
        <div className="truncate text-xs text-muted-foreground" title={sub}>{sub}</div>
      </div>
      <span className="shrink-0 text-sm text-muted-foreground tabular-nums">{ms > 0 ? clock(ms) : ""}</span>
      {children}
    </div>);
}

/** The "paste a link" way out. A fresh copy per item, so the typed text and the error never carry over. */
function LinkSection({ itemId, service, alone, onUsed }: { itemId: string; service: string; alone: boolean; onUsed: () => void }) {
    const { t } = useI18n();
    const [open, setOpen] = useState(alone);
    const [link, setLink] = useState("");
    const [error, setError] = useState("");
    const [busy, setBusy] = useState(false);
    const submit = async () => {
        setBusy(true);
        setError("");
        try {
            await submitLink(itemId, link.trim());
            onUsed();
        }
        catch (err) { setError(errorMessage(err, t("recent.unknownError"))); }
        finally { setBusy(false); }
    };
    if (!open) return (<button type="button" className="w-fit cursor-pointer text-sm text-muted-foreground underline hover:text-foreground" onClick={() => setOpen(true)}>{t("picker.pasteLink")}</button>);
    return (<form className="space-y-1" onSubmit={(e) => { e.preventDefault(); void submit(); }}>
      <div className="flex gap-2">
        <Input value={link} onChange={(e) => setLink(e.target.value)} aria-label={t("picker.linkLabel", { service })} placeholder="https://…" disabled={busy}/>
        <Button type="submit" disabled={busy || !link.trim()}>{t("picker.linkUse")}</Button>
      </div>
      {error && <p className="text-xs text-destructive">{error}</p>}
    </form>);
}

const caption = "mb-1.5 text-[10px] uppercase tracking-wide text-muted-foreground";

/** Asks which track on another service to download for a track no match was sure about. */
export function CandidatePicker({ itemId, onClose }: { itemId: string; onClose: () => void }) {
    const { t } = useI18n();
    const { label } = useSources();
    const { items } = useDownloads();
    const [currentId, setCurrentId] = useState(itemId);
    const [busy, setBusy] = useState(false);
    const reviews = items.filter((i) => i.status === "review");
    // Once the shown item is decided (the refresh after a choice lands before the call returns) the next one
    // is shown at once, so the dialog never unmounts in between; currentId catches up in the effect.
    const item = reviews.find((i) => i.id === currentId) ?? nextReview(items, currentId);
    const goNext = (from: string) => {
        const next = nextReview(items, from);
        if (next) setCurrentId(next.id);
        else onClose();
    };
    useEffect(() => {
        if (!item) onClose();
        else if (item.id !== currentId) setCurrentId(item.id);
    });
    if (!item) return null;
    const decide = async (action: () => Promise<void>) => {
        setBusy(true);
        try {
            await action();
            goNext(item.id);
        }
        catch (err) { toast.error(errorMessage(err, t("recent.unknownError"))); }
        finally { setBusy(false); }
    };
    const candidates = (item.candidates ?? []).slice(0, MAX_CANDIDATES);
    const track = item.track;
    return (<Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="sm:max-w-2xl" aria-describedby={undefined}>
        <DialogHeader><DialogTitle>{t("picker.title")}</DialogTitle></DialogHeader>
        <div>
          <p className={caption}>{t("picker.from", { service: label(track.service) })}</p>
          <div className="rounded-lg border">
            <TrackRow service={track.service} id={track.id} title={track.title} sub={meta(track.artist, track.album, track.release_date?.slice(0, 4))} image={track.image} ms={track.duration_ms}/>
          </div>
        </div>
        <div>
          {item.review_service && <p className={caption}>{t("picker.found", { service: label(item.review_service) })}</p>}
          {candidates.length > 0 ? (<div className="divide-y overflow-hidden rounded-lg border">
            {candidates.map((c) => (<TrackRow key={c.id} service={item.review_service ?? ""} id={c.id} title={c.title} sub={meta(c.artist, c.album, c.release_date?.slice(0, 4))} image={c.image} ms={c.duration_ms}
                className={c.recommended ? "bg-green-500/10" : undefined}
                badge={c.recommended && <Badge className="border-transparent bg-green-500 text-white">{t("picker.recommended")}</Badge>}>
              <span className={cn("w-11 shrink-0 text-right text-sm tabular-nums", c.recommended ? "text-green-500" : "text-amber-500")}>{Math.round(c.score * 100)}%</span>
              <Button variant={c.recommended ? "default" : "outline"} size="sm" disabled={busy} onClick={() => void decide(() => chooseCandidate(item.id, c.id))}>{t("picker.choose")}</Button>
            </TrackRow>))}
          </div>) : (<p className="text-sm text-muted-foreground">{t("picker.none")}</p>)}
        </div>
        <LinkSection key={item.id} itemId={item.id} service={label(item.review_service ?? "")} alone={candidates.length === 0} onUsed={() => goNext(item.id)}/>
        <div className="flex items-center justify-between gap-3">
          <Button variant="outline" disabled={busy} onClick={() => void decide(() => skipReview(item.id))}>{t("picker.skip")}</Button>
          <span className="text-sm text-muted-foreground">
            {t("picker.position", { n: reviews.indexOf(item) + 1, total: reviews.length })}
            {reviews.length > 1 && <> · <button type="button" className="cursor-pointer underline hover:text-foreground" onClick={() => goNext(item.id)}>{t("picker.next")}</button></>}
          </span>
        </div>
      </DialogContent>
    </Dialog>);
}
