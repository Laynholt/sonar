import { t } from "./i18n";

// New album/playlist/artist entries store a bare count; older ones kept an English caption and show as stored.
export function historyInfo({ type, info }: { type: string; info: string }): string {
    if (!/^\d+$/.test(info)) return info;
    const count = Number(info);
    if (type === "album" || type === "playlist") return t("count.tracks", { count });
    if (type === "artist") return t("count.albums", { count });
    return info;
}
