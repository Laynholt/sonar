import { useMemo, useSyncExternalStore } from "react";
import { getGuard, getSnapshot, subscribe, type DownloadGuard, type DownloadItem } from "@/lib/downloads";

export function useDownloads(): { items: DownloadItem[]; byTrackId: Map<string, DownloadItem>; activeCount: number; guard: DownloadGuard } {
    const items = useSyncExternalStore(subscribe, getSnapshot);
    const guard = useSyncExternalStore(subscribe, getGuard);
    return useMemo(() => ({
        items,
        byTrackId: new Map(items.map((item) => [item.id, item])),
        activeCount: items.filter((item) => item.status === "queued" || item.status === "downloading").length,
        guard,
    }), [items, guard]);
}
