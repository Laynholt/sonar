import { t } from "./i18n";

// New album/playlist/artist entries store a bare count; older ones kept an English caption and show as stored.
export function historyInfo({ type, info }: { type: string; info: string }): string {
    if (!/^\d+$/.test(info)) return info;
    const count = Number(info);
    if (type === "album" || type === "playlist") return t("count.tracks", { count });
    if (type === "artist") return t("count.albums", { count });
    return info;
}

// The same thing fetched by two links (an artist page with and without its name slug) was stored twice;
// the newest of each type, name and cover stays. The list comes newest first.
export function uniqueRecent<T extends { type: string; name: string; image: string }>(items: T[], limit: number): T[] {
    const seen = new Set<string>();
    return items.filter((item) => {
        const key = `${item.type}|${item.name}|${item.image}`;
        if (seen.has(key)) return false;
        seen.add(key);
        return true;
    }).slice(0, limit);
}
