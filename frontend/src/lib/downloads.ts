import { CancelAllDownloads, CancelDownload, ClearFinishedDownloads, DownloadTracks, GetDownloadGuard, GetDownloadQueue, GuardContinue, GuardWait, RetryDownload, RetryFailedDownloads } from "../../wailsjs/go/main/App";
import type { download, main } from "../../wailsjs/go/models";
import type { TrackMetadata } from "@/types/api";
import { t } from "./i18n";
import { logger } from "./logger";
import { downloadOptions, getSettings } from "./settings";
import { toastWithSound } from "./toast-with-sound";
import { errorMessage } from "./utils";

export type DownloadItem = download.Item;
export type DownloadGuard = main.DownloadGuard;
export type DownloadSource = TrackMetadata & { position: number };
export type CollectionKind = "track" | "album" | "playlist" | "artist";
export interface BatchProgress { done: number; total: number; failed: number; active: boolean }

const POLL_MS = 500;
const noGuard: DownloadGuard = { site: "", count: 0, asking: false, paused_until: 0 } as DownloadGuard;

let items: DownloadItem[] = [];
let guard = noGuard;
let key = "";
let timer: ReturnType<typeof setTimeout> | undefined;
let started = 0;
let applied = 0;
const listeners = new Set<() => void>();
// Queue ids of batches whose summary toast is still owed; the id is what Go builds: kind:id.
const batches = new Set<string>();

export function subscribe(listener: () => void) {
    listeners.add(listener);
    return () => { listeners.delete(listener); };
}
export const getSnapshot = () => items;
export const getGuard = () => guard;

const isActive = (i: DownloadItem) => i.status === "queued" || i.status === "downloading";

export function batchProgress(all: DownloadItem[], queueId: string): BatchProgress {
    const own = all.filter((i) => i.queue_id === queueId);
    return {
        done: own.filter((i) => i.status === "completed" || i.status === "skipped").length,
        total: own.length,
        failed: own.filter((i) => i.status === "failed" || i.status === "cancelled").length,
        active: own.some(isActive),
    };
}

function summarize() {
    for (const queueId of [...batches]) {
        const own = items.filter((i) => i.queue_id === queueId);
        if (own.some(isActive)) continue;
        batches.delete(queueId);
        if (!own.length) continue;
        const count = (...statuses: string[]) => own.filter((i) => statuses.includes(i.status)).length;
        const done = count("completed"), skipped = count("skipped"), failed = count("failed", "cancelled");
        const text = t("download.summary", { done, skipped, failed });
        if (!failed) toastWithSound.success(text);
        else if (done + skipped) toastWithSound.warning(text);
        else toastWithSound.error(text);
    }
}

// One timer at most: every refresh replaces it, so quick actions never stack loops.
function schedule() {
    clearTimeout(timer);
    timer = items.some(isActive) || guard.paused_until > 0 ? setTimeout(() => void refreshDownloads(), POLL_MS) : undefined;
}

export async function refreshDownloads(): Promise<void> {
    const id = ++started;
    try {
        const [nextItems, nextGuard] = await Promise.all([GetDownloadQueue(), GetDownloadGuard()]);
        if (id > applied) {
            applied = id;
            const nextKey = JSON.stringify([nextItems, nextGuard]);
            if (nextKey !== key) {
                key = nextKey;
                items = nextItems ?? [];
                guard = nextGuard;
                summarize();
                listeners.forEach((listener) => listener());
            }
        }
    }
    catch (err) {
        logger.error(errorMessage(err, "download queue refresh failed"));
    }
    schedule();
}

export function toSource(kind: CollectionKind, track: TrackMetadata, index: number): DownloadSource {
    return { ...track, position: kind === "album" || kind === "artist" ? track.track_number : index + 1 };
}
export const toSources = (kind: CollectionKind, tracks: TrackMetadata[]) => tracks.map((track, index) => toSource(kind, track, index));

export async function startDownload(collection: { kind: CollectionKind; id: string; name: string }, tracks: DownloadSource[]): Promise<void> {
    const request = {
        collection,
        options: downloadOptions(getSettings()),
        tracks: tracks.map((s) => ({
            id: s.id ?? "", title: s.name, artist: s.artists, album: s.album_name, album_artist: s.album_artist ?? "",
            release_date: s.release_date, track_number: s.track_number, disc_number: s.disc_number ?? 0, position: s.position,
        })),
    } as main.DownloadRequest;
    try {
        await DownloadTracks(request);
    }
    catch (err) {
        toastWithSound.error(errorMessage(err, t("recent.unknownError")));
        return;
    }
    batches.add(`${collection.kind}:${collection.id}`);
    await refreshDownloads();
}

const then = (action: Promise<void>) => action.then(refreshDownloads);
export const cancelDownload = (id: string) => then(CancelDownload(id));
export const cancelAllDownloads = () => then(CancelAllDownloads());
export const retryDownload = (id: string) => then(RetryDownload(id));
export const retryFailedDownloads = () => then(RetryFailedDownloads());
export const clearFinishedDownloads = () => then(ClearFinishedDownloads());
export const guardWait = () => then(GuardWait());
export const guardContinue = () => then(GuardContinue());

export async function stopQueue(queueId: string): Promise<void> {
    await Promise.all(items.filter((i) => i.queue_id === queueId && isActive(i)).map((i) => CancelDownload(i.id)));
    await refreshDownloads();
}
