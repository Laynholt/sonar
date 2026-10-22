import type { history } from "../../wailsjs/go/models";
import { useEffect, useRef, useState } from "react";
import { X, Music2, Disc3, ListMusic, UserRound, ChevronLeft, ChevronRight } from "lucide-react";
import { useI18n } from "@/lib/i18n";
import { historyInfo } from "@/lib/history-info";
interface FetchHistoryProps {
    history: history.FetchItem[];
    onSelect: (item: history.FetchItem) => void;
    onRemove: (id: string) => void;
}
export function FetchHistory({ history: items, onSelect, onRemove }: FetchHistoryProps) {
    const { t } = useI18n();
    const scroller = useRef<HTMLDivElement>(null);
    // The row in screens: how many there are and which one is in view, for the segments below it.
    const [pages, setPages] = useState({ count: 1, current: 0 });
    const overflow = pages.count > 1;
    useEffect(() => {
        const el = scroller.current;
        if (!el) return;
        const measure = () => {
            const max = el.scrollWidth - el.clientWidth;
            const count = max > 1 ? Math.ceil(el.scrollWidth / el.clientWidth) : 1;
            const current = count > 1 ? Math.round((el.scrollLeft / max) * (count - 1)) : 0;
            setPages((p) => (p.count === count && p.current === current ? p : { count, current }));
        };
        measure();
        el.addEventListener("scroll", measure, { passive: true });
        const observer = typeof ResizeObserver === "undefined" ? null : new ResizeObserver(measure);
        observer?.observe(el);
        // A plain mouse wheel scrolls the row sideways while it does not fit.
        const onWheel = (e: WheelEvent) => {
            if (el.scrollWidth <= el.clientWidth || Math.abs(e.deltaX) > Math.abs(e.deltaY)) return;
            e.preventDefault();
            el.scrollLeft += e.deltaY;
        };
        el.addEventListener("wheel", onWheel, { passive: false });
        return () => { observer?.disconnect(); el.removeEventListener("wheel", onWheel); el.removeEventListener("scroll", measure); };
    }, [items.length]);
    if (items.length === 0)
        return null;
    // A page per press; past either end it wraps to the other, so the row reads as a loop.
    const step = (dir: 1 | -1) => {
        const el = scroller.current!;
        const max = el.scrollWidth - el.clientWidth;
        const atEnd = dir > 0 ? el.scrollLeft >= max - 1 : el.scrollLeft <= 1;
        el.scrollTo({ left: atEnd ? (dir > 0 ? 0 : max) : el.scrollLeft + dir * el.clientWidth * 0.8, behavior: "smooth" });
    };
    const goTo = (page: number) => {
        const el = scroller.current!;
        el.scrollTo({ left: (page / (pages.count - 1)) * (el.scrollWidth - el.clientWidth), behavior: "smooth" });
    };
    const arrow = (dir: 1 | -1) => (<button type="button" aria-label={t(dir > 0 ? "recent.next" : "recent.prev")} className="shrink-0 h-8 w-8 rounded-full border bg-card hover:bg-accent flex items-center justify-center cursor-pointer" onClick={() => step(dir)}>
        {dir > 0 ? <ChevronRight className="h-4 w-4"/> : <ChevronLeft className="h-4 w-4"/>}
      </button>);
    const getTypeLabel = (type: string) => {
        switch (type) {
            case "track":
                return t("type.track");
            case "album":
                return t("type.album");
            case "playlist":
                return t("type.playlist");
            case "artist":
                return t("type.artist");
            default:
                return type;
        }
    };
    const getTypeIcon = (type: string) => {
        switch (type) {
            case "track":
                return Music2;
            case "album":
                return Disc3;
            case "playlist":
                return ListMusic;
            case "artist":
                return UserRound;
            default:
                return null;
        }
    };
    const getTypeBadgeClass = (type: string) => {
        switch (type) {
            case "track":
                return "bg-blue-500/10 text-blue-600 dark:bg-blue-500/20 dark:text-blue-400";
            case "album":
                return "bg-green-500/10 text-green-600 dark:bg-green-500/20 dark:text-green-400";
            case "playlist":
                return "bg-purple-500/10 text-purple-600 dark:bg-purple-500/20 dark:text-purple-400";
            case "artist":
                return "bg-orange-500/10 text-orange-600 dark:bg-orange-500/20 dark:text-orange-400";
            default:
                return "bg-muted text-muted-foreground";
        }
    };
    return (<div className="space-y-2">
      <p className="text-sm text-muted-foreground text-center">{t("recent.title", { count: items.length })}</p>
      <div className="flex items-center gap-2">
      {overflow && arrow(-1)}
      <div ref={scroller} className="flex-1 min-w-0 overflow-x-auto no-scrollbar">
      <div className="flex gap-2 w-max mx-auto px-2 pb-2 pt-2">
        {items.map((item) => (<div key={item.id} className="relative shrink-0 w-[130px] group cursor-pointer rounded-lg border bg-card hover:bg-accent transition-colors overflow-visible" onClick={() => onSelect(item)}>
            <button type="button" aria-label={t("recent.remove", { name: item.name })} className="absolute -top-1.5 -right-1.5 z-10 w-5 h-5 rounded-full bg-red-500 hover:bg-red-600 flex items-center justify-center opacity-0 group-hover:opacity-100 transition-all cursor-pointer shadow-sm" onClick={(e) => {
                e.stopPropagation();
                onRemove(item.id);
            }}>
              <X className="h-3 w-3 text-red-900" strokeWidth={3}/>
            </button>
            <div className="p-2">
              <div className="aspect-square w-full rounded-md overflow-hidden mb-2 bg-muted">
                {item.image ? (<img src={item.image} alt={item.name} className="w-full h-full object-cover"/>) : (<div className="w-full h-full flex items-center justify-center text-muted-foreground text-xs">
                    {t("recent.noImage")}
                  </div>)}
              </div>
              <div className="space-y-0.5">
                <p className="text-xs font-medium truncate" title={item.name}>
                  {item.name}
                </p>
                <p className="text-xs text-muted-foreground truncate" title={historyInfo(item)}>
                  {historyInfo(item)}
                </p>
                {(() => {
                const IconComponent = getTypeIcon(item.type);
                return (<span className={`inline-flex items-center gap-1 text-[10px] px-1.5 py-0.5 rounded ${getTypeBadgeClass(item.type)}`}>
                      {IconComponent ? <IconComponent className="h-2.5 w-2.5"/> : null}
                      {getTypeLabel(item.type)}
                    </span>);
            })()}
              </div>
            </div>
          </div>))}
      </div>
      </div>
      {overflow && arrow(1)}
      </div>
      {overflow && (<div className="flex justify-center gap-1.5">
          {Array.from({ length: pages.count }, (_, page) => (<button key={page} type="button" aria-label={t("recent.page", { page: page + 1 })} aria-current={page === pages.current} className="h-4 flex items-center cursor-pointer group/seg" onClick={() => goTo(page)}>
              <span className={`block h-1 rounded-full transition-all duration-300 ${page === pages.current ? "w-8 bg-primary" : "w-4 bg-muted-foreground/30 group-hover/seg:bg-muted-foreground/60"}`}/>
            </button>))}
        </div>)}
    </div>);
}
