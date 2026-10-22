import { useMemo, useSyncExternalStore } from "react";
import { getHeld, getSites, getSnapshot, subscribe, type DownloadItem } from "@/lib/downloads";
import type { source } from "../../wailsjs/go/models";

export function useDownloads(): { items: DownloadItem[]; byTrackId: Map<string, DownloadItem>; activeCount: number; sites: source.Status[]; held: boolean } {
    const items = useSyncExternalStore(subscribe, getSnapshot);
    const sites = useSyncExternalStore(subscribe, getSites);
    const held = useSyncExternalStore(subscribe, getHeld);
    return useMemo(() => ({
        items,
        byTrackId: new Map(items.map((item) => [item.id, item])),
        activeCount: items.filter((item) => item.status === "matching" || item.status === "queued" || item.status === "downloading").length,
        sites,
        held,
    }), [items, sites, held]);
}
