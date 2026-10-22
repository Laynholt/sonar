import { useEffect, useReducer } from "react";
import { clock, siteName } from "@/lib/download-format";
import type { DownloadItem } from "@/lib/downloads";
import { useI18n } from "@/lib/i18n";
import { cn } from "@/lib/utils";
import type { source } from "../../wailsjs/go/models";

/** What each download site is doing right now; a site with nothing going on shows as ready, so the card keeps its shape. */
export function SiteRows({ sites, items }: { sites: source.Status[]; items: DownloadItem[] }) {
    const { t } = useI18n();
    // Re-render every second while a pause counts down; the clock itself is read at render time.
    const [, tick] = useReducer((n: number) => n + 1, 0);
    const now = Date.now();
    const counting = sites.some((s) => s.paused_until > now);
    useEffect(() => {
        if (!counting) return;
        const id = setInterval(tick, 1000);
        return () => clearInterval(id);
    }, [counting]);
    const rows = sites.flatMap((s) => {
        if (items.some((i) => i.status === "downloading" && i.site === s.name)) return [{ s, dot: "bg-green-500", label: t(s.limit > 0 ? "widget.siteBusy" : "widget.siteBusyNoLimit", { count: s.count, limit: s.limit }) }];
        if (s.paused_until > now) return [{ s, dot: "bg-amber-500", label: t("widget.sitePaused", { time: clock(s.paused_until - now) }) }];
        if (s.asking) return [{ s, dot: "bg-amber-500", label: t("widget.siteAsking") }];
        if (!s.available) return [{ s, dot: "bg-muted-foreground", label: t("widget.siteDown") }];
        return [{ s, dot: "bg-muted-foreground/40", label: t("widget.siteReady") }];
    });
    if (!rows.length) return null;
    return (<ul className="rounded-md border divide-y text-sm">
      {rows.map(({ s, dot, label }) => (<li key={s.name} className="flex items-center gap-2 px-2.5 py-2">
        <span className={cn("h-2 w-2 rounded-full shrink-0", dot)}/>
        <span className="truncate">{siteName(s.name)}</span>
        <span className="ml-auto shrink-0 text-muted-foreground tabular-nums">{label}</span>
      </li>))}
    </ul>);
}
