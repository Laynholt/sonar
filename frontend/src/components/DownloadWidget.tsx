import { useCallback, useEffect, useLayoutEffect, useRef, useState, type PointerEvent } from "react";
import { Download, X } from "lucide-react";
import { DownloadBadge } from "@/components/DownloadBadge";
import { SiteRows } from "@/components/SiteRows";
import { Progress } from "@/components/ui/progress";
import { useDownloadSpeed } from "@/hooks/useDownloadSpeed";
import { useDownloads } from "@/hooks/useDownloads";
import { mb } from "@/lib/download-format";
import { useI18n } from "@/lib/i18n";
import { openPicker } from "@/lib/picker";
import type { DownloadItem } from "@/lib/downloads";

const isActive = (i: DownloadItem) => i.status === "queued" || i.status === "downloading";

// Top corners sit below the title bar, left ones right of the sidebar.
const CORNERS = {
    "top-left": "top-14 left-[4.5rem]",
    "top-right": "top-14 right-4",
    "bottom-left": "bottom-4 left-[4.5rem]",
    "bottom-right": "bottom-4 right-4",
};
type Corner = keyof typeof CORNERS;
const CORNER_KEY = "sonar.downloadWidgetCorner";
function loadCorner(): Corner {
    try {
        const saved = localStorage.getItem(CORNER_KEY);
        if (saved && saved in CORNERS) return saved as Corner;
    } catch { /* storage blocked: use the default */ }
    return "bottom-right";
}

// Slides the card from where it was dropped to where its corner puts it.
function settle(el: HTMLElement, from: DOMRect) {
    el.style.transition = "none";
    el.style.transform = "";
    const to = el.getBoundingClientRect();
    el.style.transform = `translate(${from.left - to.left}px, ${from.top - to.top}px)`;
    void el.offsetWidth;
    el.style.transition = "transform 200ms ease-out";
    el.style.transform = "";
}

// A run starts when downloads appear from idle. It counts the items new since then and the old ones
// that were queued again, so finished items left from earlier runs stay out of its numbers.
// X collapses a running run to a badge and closes a finished one.
interface Run { before: Set<string>; seen: Set<string>; mode: "open" | "collapsed" | "closed" }

/** Card with the current run's progress, speed and size; it can be dragged to any corner. */
export function DownloadWidget({ hidden, onOpen }: { hidden: boolean; onOpen: () => void }) {
    const { t, language } = useI18n();
    const { items, activeCount, sites } = useDownloads();
    const speed = useDownloadSpeed(items);
    const [run, setRun] = useState<Run | null>(null);
    const prevItems = useRef(items);
    const wasActive = useRef(false);
    useEffect(() => {
        const before = prevItems.current;
        prevItems.current = items;
        const started = activeCount > 0 && !wasActive.current;
        wasActive.current = activeCount > 0;
        setRun((old) => {
            // Downloads appearing from idle start a new, expanded run, however the last one ended.
            const r = started ? null : old;
            if (!r && activeCount === 0) return r;
            if (r?.mode === "closed" && activeCount === 0) return null;
            const next = r ?? { before: new Set(before.map((i) => i.id)), seen: new Set<string>(), mode: "open" as const };
            const active = items.filter(isActive).map((i) => i.id);
            return active.some((id) => !next.seen.has(id)) ? { ...next, seen: new Set([...next.seen, ...active]) } : next;
        });
    }, [items, activeCount, run?.mode]);

    const card = useRef<HTMLElement | null>(null);
    const cardRef = useCallback((el: HTMLElement | null) => { card.current = el; }, []);
    const drag = useRef<{ x: number; y: number } | null>(null);
    const dropped = useRef<DOMRect | null>(null);
    const [corner, setCorner] = useState(loadCorner);
    useLayoutEffect(() => {
        if (dropped.current && card.current) settle(card.current, dropped.current);
        dropped.current = null;
    }, [corner]);
    const onPointerDown = (e: PointerEvent<HTMLElement>) => {
        // The badge is itself a button; the card's buttons stay clickable.
        const inner = (e.target as HTMLElement).closest("button");
        if (e.button !== 0 || (inner && inner !== e.currentTarget)) return;
        drag.current = { x: e.clientX, y: e.clientY };
        e.currentTarget.setPointerCapture?.(e.pointerId);
    };
    const onPointerMove = (e: PointerEvent<HTMLElement>) => {
        if (!drag.current) return;
        e.currentTarget.style.transition = "none";
        e.currentTarget.style.transform = `translate(${e.clientX - drag.current.x}px, ${e.clientY - drag.current.y}px)`;
    };
    const onPointerUp = (e: PointerEvent<HTMLElement>) => {
        if (!drag.current) return;
        drag.current = null;
        const r = e.currentTarget.getBoundingClientRect();
        const next = `${r.top + r.height / 2 < window.innerHeight / 2 ? "top" : "bottom"}-${r.left + r.width / 2 < window.innerWidth / 2 ? "left" : "right"}` as Corner;
        try { localStorage.setItem(CORNER_KEY, next); } catch { /* the corner just isn't remembered */ }
        if (next === corner) settle(e.currentTarget, r);
        else { dropped.current = r; setCorner(next); }
    };

    if (!run || run.mode === "closed" || hidden) return null;
    const own = items.filter((i) => run.seen.has(i.id) || !run.before.has(i.id));
    if (!own.length) return null;
    const count = (...statuses: string[]) => own.filter((i) => statuses.includes(i.status)).length;
    const done = count("completed", "skipped", "ignored", "not_found"), failed = count("failed", "cancelled"), queued = count("queued");
    // Every review item, not only this run's: it is the number the picker counts through.
    const review = items.filter((i) => i.status === "review").length;
    const current = own.filter((i) => i.status === "downloading");
    const partial = current.reduce((sum, i) => sum + (i.size ? i.bytes / i.size : 0), 0);
    const total = own.reduce((sum, i) => sum + (i.bytes ?? 0), 0);
    const running = activeCount > 0;
    if (run.mode === "collapsed") {
        const expand = () => setRun({ ...run, mode: "open" });
        // A press that barely moved is a click; a longer one was a drag to another corner.
        const onBadgeUp = (e: PointerEvent<HTMLElement>) => {
            const moved = drag.current ? Math.hypot(e.clientX - drag.current.x, e.clientY - drag.current.y) : Infinity;
            onPointerUp(e);
            if (moved < 4) expand();
        };
        return (<DownloadBadge ref={cardRef} className={`fixed ${CORNERS[corner]} z-40`} progress={(done + failed + partial) / own.length} speed={mb(speed.total, language)}
            done={!running} attention={review > 0 || sites.some((s) => s.asking)} onExpand={expand}
            onPointerDown={onPointerDown} onPointerMove={onPointerMove} onPointerUp={onBadgeUp} onPointerCancel={onPointerUp}/>);
    }
    return (<div ref={cardRef} role="status" className={`fixed ${CORNERS[corner]} z-40 w-80 rounded-lg border bg-card shadow-lg p-3 space-y-3 text-sm cursor-grab active:cursor-grabbing select-none touch-none`}
        onPointerDown={onPointerDown} onPointerMove={onPointerMove} onPointerUp={onPointerUp} onPointerCancel={onPointerUp}>
      <div className="flex items-center gap-2">
        <Download className="h-4 w-4 shrink-0 text-primary"/>
        <span className="font-medium flex-1 truncate">{t(running ? "widget.progress" : "widget.finished", { done, total: own.length })}</span>
        <button type="button" aria-label={t("widget.hide")} className="h-6 w-6 rounded hover:bg-accent flex items-center justify-center cursor-pointer" onClick={() => setRun({ ...run, mode: running ? "collapsed" : "closed" })}>
          <X className="h-4 w-4"/>
        </button>
      </div>
      <Progress value={(done + failed + partial) / own.length}/>
      {current.slice(0, 3).map((item) => (<div key={item.id} className="space-y-1">
        <div className="flex items-baseline gap-2">
          <span className="flex-1 truncate" title={`${item.track.title} — ${item.track.artist}`}>{item.track.title} <span className="text-muted-foreground">— {item.track.artist}</span></span>
          {item.bytes > 0 && <span className="shrink-0 text-xs text-muted-foreground tabular-nums">{item.size ? t("widget.sizeOf", { done: mb(item.bytes, language), size: mb(item.size, language) }) : t("widget.size", { size: mb(item.bytes, language) })}</span>}
        </div>
        {item.message && <p className="text-xs text-muted-foreground truncate">{item.message}</p>}
        {item.size > 0 && <Progress value={item.bytes / item.size}/>}
      </div>))}
      {review > 0 && (<div className="flex items-center gap-2 rounded-md border border-amber-500/30 bg-amber-500/10 p-2 text-amber-500">
        <span className="flex-1">{t("widget.review", { count: review })}</span>
        <button type="button" className="shrink-0 underline cursor-pointer" onClick={() => openPicker()}>{t("widget.reviewOpen")}</button>
      </div>)}
      <SiteRows sites={sites} items={items}/>
      <div className="flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-muted-foreground tabular-nums">
        {running && <span>{t("widget.speed", { speed: mb(speed.total, language) })}</span>}
        <span>{t("widget.downloaded", { size: mb(total, language) })}</span>
        {queued > 0 && <span>{t("widget.waiting", { count: queued })}</span>}
        {failed > 0 && <span className="text-destructive">{t("widget.failed", { count: failed })}</span>}
        <button type="button" className="ml-auto underline cursor-pointer hover:text-foreground" onClick={onOpen}>{t("widget.open")}</button>
      </div>
    </div>);
}
