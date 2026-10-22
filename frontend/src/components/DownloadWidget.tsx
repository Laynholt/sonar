import { useEffect, useRef, useState } from "react";
import { Download, X } from "lucide-react";
import { useDownloads } from "@/hooks/useDownloads";
import { useI18n } from "@/lib/i18n";
import type { DownloadItem } from "@/lib/downloads";

const isActive = (i: DownloadItem) => i.status === "queued" || i.status === "downloading";
const MB = 1024 * 1024;

// A run starts when downloads appear from idle. It counts the items new since then and the old ones
// that were queued again, so finished items left from earlier runs stay out of its numbers.
interface Run { before: Set<string>; seen: Set<string>; closed: boolean }

function Bar({ value }: { value: number }) {
    return (<div className="h-1.5 w-full rounded-full bg-muted overflow-hidden">
      <div className="h-full bg-primary transition-[width] duration-500" style={{ width: `${Math.min(100, Math.max(0, value * 100))}%` }}/>
    </div>);
}

/** Bottom-right card with the current run's progress, speed and size. */
export function DownloadWidget({ hidden, onOpen }: { hidden: boolean; onOpen: () => void }) {
    const { t, language } = useI18n();
    const { items, activeCount } = useDownloads();
    const [run, setRun] = useState<Run | null>(null);
    const [speed, setSpeed] = useState(0);
    const prev = useRef<{ items: DownloadItem[]; bytes: number; at: number }>({ items, bytes: 0, at: Date.now() });
    useEffect(() => {
        const last = prev.current;
        setRun((r) => {
            if (!r && activeCount === 0) return r;
            if (r?.closed && activeCount === 0) return null;
            const next = r ?? { before: new Set(last.items.map((i) => i.id)), seen: new Set<string>(), closed: false };
            const active = items.filter(isActive).map((i) => i.id);
            return active.some((id) => !next.seen.has(id)) ? { ...next, seen: new Set([...next.seen, ...active]) } : next;
        });
        // Bytes come in with each poll; the speed is smoothed over the last few.
        const bytes = items.reduce((sum, i) => sum + (i.bytes ?? 0), 0);
        const now = Date.now();
        const seconds = (now - last.at) / 1000;
        const downloading = items.some((i) => i.status === "downloading");
        if (seconds > 0) setSpeed((s) => (downloading ? 0.5 * s + 0.5 * Math.max(0, bytes - last.bytes) / seconds : 0));
        prev.current = { items, bytes, at: now };
    }, [items, activeCount]);

    if (!run || run.closed || hidden) return null;
    const own = items.filter((i) => run.seen.has(i.id) || !run.before.has(i.id));
    if (!own.length) return null;
    const count = (...statuses: string[]) => own.filter((i) => statuses.includes(i.status)).length;
    const done = count("completed", "skipped"), failed = count("failed", "cancelled"), queued = count("queued");
    const current = own.filter((i) => i.status === "downloading");
    const partial = current.reduce((sum, i) => sum + (i.size ? i.bytes / i.size : 0), 0);
    const mb = (bytes: number) => (bytes / MB).toLocaleString(language, { maximumFractionDigits: 1, minimumFractionDigits: 1 });
    const total = own.reduce((sum, i) => sum + (i.bytes ?? 0), 0);
    const running = activeCount > 0;
    return (<div role="status" className="fixed bottom-4 right-4 z-40 w-80 rounded-lg border bg-card shadow-lg p-3 space-y-3 text-sm">
      <div className="flex items-center gap-2">
        <Download className="h-4 w-4 shrink-0 text-primary"/>
        <span className="font-medium flex-1 truncate">{t(running ? "widget.progress" : "widget.finished", { done, total: own.length })}</span>
        <button type="button" aria-label={t("widget.hide")} className="h-6 w-6 rounded hover:bg-accent flex items-center justify-center cursor-pointer" onClick={() => setRun({ ...run, closed: true })}>
          <X className="h-4 w-4"/>
        </button>
      </div>
      <Bar value={(done + failed + partial) / own.length}/>
      {current.slice(0, 3).map((item) => (<div key={item.id} className="space-y-1">
        <div className="flex items-baseline gap-2">
          <span className="flex-1 truncate" title={`${item.track.title} — ${item.track.artist}`}>{item.track.title} <span className="text-muted-foreground">— {item.track.artist}</span></span>
          {item.bytes > 0 && <span className="shrink-0 text-xs text-muted-foreground tabular-nums">{item.size ? t("widget.sizeOf", { done: mb(item.bytes), size: mb(item.size) }) : t("widget.size", { size: mb(item.bytes) })}</span>}
        </div>
        {item.message && <p className="text-xs text-muted-foreground truncate">{item.message}</p>}
        {item.size > 0 && <Bar value={item.bytes / item.size}/>}
      </div>))}
      <div className="flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-muted-foreground tabular-nums">
        {running && <span>{t("widget.speed", { speed: mb(speed) })}</span>}
        <span>{t("widget.downloaded", { size: mb(total) })}</span>
        {queued > 0 && <span>{t("widget.waiting", { count: queued })}</span>}
        {failed > 0 && <span className="text-destructive">{t("widget.failed", { count: failed })}</span>}
        <button type="button" className="ml-auto underline cursor-pointer hover:text-foreground" onClick={onOpen}>{t("widget.open")}</button>
      </div>
    </div>);
}
