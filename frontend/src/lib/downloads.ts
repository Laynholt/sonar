import { CancelAllDownloads, CancelDownload, ChooseCandidate, ClearFinishedDownloads, ClearQueue, DownloadTracks, GetDownloadState, GuardContinue, GuardWait, ResumeQueue, RetryDownload, RetryFailedDownloads, SkipReview, UseAmazonLink } from "../../wailsjs/go/main/App";
import type { download, main, source } from "../../wailsjs/go/models";
import type { TrackMetadata } from "@/types/api";
import { openFFmpegInstall } from "./ffmpeg-install";
import { t } from "./i18n";
import { logger } from "./logger";
import { downloadOptions, getSettings } from "./settings";
import { toastWithSound } from "./toast-with-sound";
import { errorMessage } from "./utils";

export type DownloadItem = download.Item;
export type DownloadSource = TrackMetadata & { position: number };
export type CollectionKind = "track" | "album" | "playlist" | "artist";
export interface BatchProgress { done: number; total: number; failed: number; review: number; active: boolean }

const POLL_MS = 500;

let items: DownloadItem[] = [];
let sites: source.Status[] = [];
let held = false;
let key = "";
let timer: ReturnType<typeof setTimeout> | undefined;
let started = 0;
let applied = 0;
const listeners = new Set<() => void>();
// Batches whose summary toast is still owed, keyed by the queue id Go builds (kind:id). A batch is the
// track ids one start asked for, so older runs in the same queue are not counted; only refreshes
// numbered above `after` (started once the enqueue finished) may close it.
// A retry after the summary is silent by design: the batch is gone by then.
const batches = new Map<string, { ids: Set<string>; after: number }>();

export function subscribe(listener: () => void) {
    listeners.add(listener);
    return () => { listeners.delete(listener); };
}
export const getSnapshot = () => items;
export const getSites = () => sites;
export const getHeld = () => held;

// A review item waits for the user, so it does not count as active.
const isActive = (i: DownloadItem) => i.status === "matching" || i.status === "queued" || i.status === "downloading";

export function batchProgress(all: DownloadItem[], queueId: string): BatchProgress {
    const ids = batches.get(queueId)?.ids;
    const own = all.filter((i) => i.queue_id === queueId && (!ids || ids.has(i.id)));
    return {
        done: own.filter((i) => i.status === "completed" || i.status === "skipped" || i.status === "ignored" || i.status === "not_found").length,
        total: own.length,
        failed: own.filter((i) => i.status === "failed" || i.status === "cancelled").length,
        review: own.filter((i) => i.status === "review").length,
        active: own.some(isActive),
    };
}

function summarize(refreshId: number) {
    for (const [queueId, batch] of [...batches]) {
        // A response from before the enqueue shows the previous run's statuses.
        if (refreshId <= batch.after) continue;
        const own = items.filter((i) => i.queue_id === queueId && batch.ids.has(i.id));
        if (own.some(isActive)) continue;
        batches.delete(queueId);
        if (!own.length) continue;
        const count = (...statuses: string[]) => own.filter((i) => statuses.includes(i.status)).length;
        const done = count("completed"), skipped = count("skipped", "ignored"), failed = count("failed", "cancelled");
        const review = count("review"), notFound = count("not_found");
        const text = [t("download.summary", { done, skipped, failed }), review && t("download.summaryReview", { count: review }), notFound && t("download.summaryNotFound", { count: notFound })].filter(Boolean).join(" · ");
        if (!failed) toastWithSound.success(text);
        else if (done + skipped) toastWithSound.warning(text);
        else toastWithSound.error(text);
    }
}

// One timer at most: every refresh replaces it, so quick actions never stack loops.
function schedule() {
    clearTimeout(timer);
    timer = items.some(isActive) || batches.size > 0 || sites.some((s) => s.paused_until > 0) ? setTimeout(() => void refreshDownloads(), POLL_MS) : undefined;
}

export async function refreshDownloads(): Promise<void> {
    const id = ++started;
    try {
        const state = await GetDownloadState();
        if (id > applied) {
            applied = id;
            const nextKey = JSON.stringify(state);
            if (nextKey !== key) {
                key = nextKey;
                items = state.items ?? [];
                sites = state.sites ?? [];
                held = state.held;
                listeners.forEach((listener) => listener());
            }
            summarize(id);
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
    if (!collection.id) return;
    const request = {
        collection,
        options: downloadOptions(getSettings()),
        tracks: tracks.map((s) => ({
            id: s.id ?? "", title: s.name, artist: s.artists, album: s.album_name, album_artist: s.album_artist ?? "",
            release_date: s.release_date, track_number: s.track_number, disc_number: s.disc_number ?? 0, position: s.position,
            service: s.service, album_id: s.album_id ?? "", duration_ms: s.duration_ms, explicit: !!s.is_explicit, image: s.images,
        })),
    } as main.DownloadRequest;
    // Registered before the call: the first poll may already see the items and must not miss the batch.
    const queueId = `${collection.kind}:${collection.id}`;
    const previous = batches.get(queueId);
    batches.set(queueId, { ids: new Set([...(previous?.ids ?? []), ...request.tracks.map((x) => x.id)]), after: Infinity });
    try {
        await DownloadTracks(request);
    }
    catch (err) {
        if (previous) batches.set(queueId, previous);
        else batches.delete(queueId);
        // Go refuses an MP3/M4A request when FFmpeg is missing; the install dialog replaces the error, and a finished install sends the request again.
        if (errorMessage(err, "").startsWith("NO_FFMPEG: ")) {
            openFFmpegInstall(() => void startDownload(collection, tracks));
            return;
        }
        toastWithSound.error(errorMessage(err, t("recent.unknownError")));
        return;
    }
    batches.get(queueId)!.after = started;
    await refreshDownloads();
}

const then = (action: Promise<void>) => action.then(refreshDownloads);
export const cancelDownload = (id: string) => then(CancelDownload(id));
export const cancelAllDownloads = () => then(CancelAllDownloads());
export const retryDownload = (id: string) => then(RetryDownload(id));
export const retryFailedDownloads = () => then(RetryFailedDownloads());
export const clearFinishedDownloads = () => then(ClearFinishedDownloads());
export const chooseCandidate = (id: string, amazonId: string) => then(ChooseCandidate(id, amazonId));
export const useAmazonLink = (id: string, link: string) => then(UseAmazonLink(id, link));
export const skipReview = (id: string) => then(SkipReview(id));
export const resumeQueue = () => then(ResumeQueue());
export const clearQueue = () => then(ClearQueue());
export const guardWait = (site: string, minutes: number) => then(GuardWait(site, minutes));
export const guardContinue = (site: string) => then(GuardContinue(site));

/** The artist's discography tracks, then the top tracks that are not on a listed album; each track id once, first wins. */
export const artistTracks = (a: { track_list: TrackMetadata[]; top_tracks?: TrackMetadata[]; album_list: { id: string }[] }) => {
    const listed = new Set(a.album_list.map((x) => x.id));
    const seen = new Set<string>();
    // top_tracks is missing in artist payloads saved in history before it existed.
    return [...a.track_list, ...(a.top_tracks ?? []).filter((x) => !listed.has(x.album_id ?? ""))]
        .filter((x) => !x.id || (!seen.has(x.id) && seen.add(x.id)));
};

export async function stopQueue(queueId: string): Promise<void> {
    await Promise.all(items.filter((i) => i.queue_id === queueId && isActive(i)).map((i) => CancelDownload(i.id)));
    await refreshDownloads();
}
