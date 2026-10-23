// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { TrackMetadata } from "@/types/api";
import type { source } from "../../wailsjs/go/models";
import type { DownloadItem } from "./downloads";

vi.mock("../../wailsjs/go/main/App", () => ({
    DownloadTracks: vi.fn(), GetDownloadState: vi.fn(),
    ChooseCandidate: vi.fn(), UseAmazonLink: vi.fn(), SkipReview: vi.fn(), ResumeQueue: vi.fn(), ClearQueue: vi.fn(),
    CancelDownload: vi.fn(), CancelAllDownloads: vi.fn(), RetryDownload: vi.fn(),
    RetryFailedDownloads: vi.fn(), ClearFinishedDownloads: vi.fn(), GuardWait: vi.fn(), GuardContinue: vi.fn(),
    LoadSettings: vi.fn(), SaveSettings: vi.fn(),
}));
vi.mock("@/lib/ffmpeg-install", () => ({ openFFmpegInstall: vi.fn() }));
vi.mock("@/lib/toast-with-sound", () => ({ toastWithSound: { success: vi.fn(), warning: vi.fn(), error: vi.fn() } }));

const idle = { name: "", available: true, needs_check: false, reason: "", count: 0, limit: 0, asking: false, paused_until: 0 } as source.Status;
const track = (n: number): TrackMetadata => ({
    service: "amazon", id: `B0${n}`, name: `Song ${n}`, artists: "Artist", album_name: "Album", album_artist: "Album Artist",
    duration_ms: 1000, images: "", release_date: "2020", track_number: n + 4, disc_number: 2, external_urls: "",
});
const item = (id: string, status: string, queue_id = "album:A1") => ({ id, queue_id, track: {}, status, message: "", path: "", error: "" }) as DownloadItem;

const state = (api: typeof import("../../wailsjs/go/main/App"), items: DownloadItem[], sites: source.Status[] = [], held = false) =>
    vi.mocked(api.GetDownloadState).mockResolvedValue({ items, sites, held } as Awaited<ReturnType<typeof api.GetDownloadState>>);

async function load() {
    vi.resetModules();
    const api = await import("../../wailsjs/go/main/App");
    vi.mocked(api.DownloadTracks).mockResolvedValue();
    state(api, []);
    const toast = (await import("@/lib/toast-with-sound")).toastWithSound;
    return { api, toast, store: await import("./downloads"), settings: await import("./settings") };
}

beforeEach(() => { vi.clearAllMocks(); vi.useFakeTimers(); });
afterEach(() => { vi.clearAllTimers(); vi.useRealTimers(); });

describe("downloads store", () => {
    it("sends an album with the saved options and position = track_number", async () => {
        const { api, store, settings } = await load();
        const sources = store.toSources("album", [track(1), track(2)]);

        await store.startDownload({ kind: "album", id: "A1", name: "Album" }, sources);

        const req = vi.mocked(api.DownloadTracks).mock.calls[0][0];
        expect(req.collection).toEqual({ kind: "album", id: "A1", name: "Album" });
        expect(req.options).toEqual(settings.downloadOptions(settings.getSettings()));
        expect(req.options).toHaveProperty("source");
        expect(req.tracks[0]).toEqual({ id: "B01", title: "Song 1", artist: "Artist", album: "Album", album_artist: "Album Artist", release_date: "2020", track_number: 5, disc_number: 2, position: 5, service: "amazon", album_id: "", duration_ms: 1000, explicit: false, image: "" });
        expect(req.tracks.map((t) => t.position)).toEqual([5, 6]);
    });

    it("a missing FFmpeg opens the install instead of a toast, and installing sends the same request again", async () => {
        const { api, toast, store } = await load();
        const { openFFmpegInstall } = await import("@/lib/ffmpeg-install");
        vi.mocked(api.DownloadTracks).mockRejectedValueOnce("NO_FFMPEG: FFmpeg is needed for MP3");

        await store.startDownload({ kind: "album", id: "A1", name: "Album" }, store.toSources("album", [track(1)]));

        expect(toast.error).not.toHaveBeenCalled();
        expect(openFFmpegInstall).toHaveBeenCalledTimes(1);
        expect(api.DownloadTracks).toHaveBeenCalledTimes(1);
        vi.mocked(openFFmpegInstall).mock.calls[0][0]!("C:\ff\ffmpeg.exe");
        await vi.advanceTimersByTimeAsync(0);
        expect(api.DownloadTracks).toHaveBeenCalledTimes(2);
        expect(vi.mocked(api.DownloadTracks).mock.calls[1][0].tracks).toEqual(vi.mocked(api.DownloadTracks).mock.calls[0][0].tracks);
    });

    it("numbers playlist and track positions by index + 1", async () => {
        const { store } = await load();

        expect(store.toSources("playlist", [track(1), track(2), track(3)]).map((s) => s.position)).toEqual([1, 2, 3]);
        expect(store.toSource("track", track(9), 6).position).toBe(7);
        expect(store.toSource("artist", track(9), 0).position).toBe(13);
    });

    it("polls while something is active, then stops", async () => {
        const { api, store } = await load();
        state(api, [item("B01", "downloading")]);
        await store.startDownload({ kind: "album", id: "A1", name: "Album" }, store.toSources("album", [track(1)]));
        const callsAfterStart = vi.mocked(api.GetDownloadState).mock.calls.length;

        await vi.advanceTimersByTimeAsync(1600);
        expect(vi.mocked(api.GetDownloadState).mock.calls.length).toBe(callsAfterStart + 3);

        state(api, [item("B01", "completed")]);
        await vi.advanceTimersByTimeAsync(600);
        const settled = vi.mocked(api.GetDownloadState).mock.calls.length;
        await vi.advanceTimersByTimeAsync(5000);
        expect(vi.mocked(api.GetDownloadState).mock.calls.length).toBe(settled);
    });

    it("keeps polling during a guard pause and never runs two loops", async () => {
        const { api, store } = await load();
        state(api, [], [{ ...idle, name: "lucida", count: 50, paused_until: Date.now() + 60_000 }]);
        await Promise.all([store.refreshDownloads(), store.refreshDownloads(), store.refreshDownloads()]);
        const before = vi.mocked(api.GetDownloadState).mock.calls.length;

        await vi.advanceTimersByTimeAsync(1000);

        expect(vi.mocked(api.GetDownloadState).mock.calls.length - before).toBe(2);
        expect(store.getSites()[0].paused_until).toBeGreaterThan(0);
    });

    it("reflects the sites and held flag and keeps snapshots stable until the data changes", async () => {
        const { api, store } = await load();
        const sites = [{ ...idle, name: "lucida", count: 50, limit: 50, asking: true }];
        state(api, [], sites, true);
        const listener = vi.fn();
        store.subscribe(listener);

        await store.refreshDownloads();
        const items = store.getSnapshot();
        const g = store.getSites();
        await store.refreshDownloads();

        expect(g).toEqual(sites);
        expect(store.getHeld()).toBe(true);
        expect(store.getSnapshot()).toBe(items);
        expect(store.getSites()).toBe(g);
        expect(listener).toHaveBeenCalledTimes(1);
    });

    it("shows one summary toast when the batch finishes", async () => {
        const { api, toast, store } = await load();
        state(api, [item("B01", "downloading"), item("B02", "queued"), item("B03", "queued")]);
        await store.startDownload({ kind: "album", id: "A1", name: "Album" }, store.toSources("album", [track(1), track(2), track(3)]));
        expect(toast.success).not.toHaveBeenCalled();

        state(api, [item("B01", "completed"), item("B02", "completed"), item("B03", "skipped")]);
        await vi.advanceTimersByTimeAsync(600);
        await vi.advanceTimersByTimeAsync(3000);

        expect(toast.success).toHaveBeenCalledTimes(1);
        expect(toast.success).toHaveBeenCalledWith("Downloaded 2 · skipped 1 · failed 0");
    });

    it("warns on partial failure and errors when nothing downloaded", async () => {
        const { api, toast, store } = await load();
        state(api, [item("B01", "completed"), item("B02", "failed"), item("B03", "cancelled")]);
        await store.startDownload({ kind: "album", id: "A1", name: "Album" }, store.toSources("album", [track(1), track(2), track(3)]));
        expect(toast.warning).toHaveBeenCalledWith("Downloaded 1 · skipped 0 · failed 2");

        state(api, [item("B04", "failed", "playlist:P1")]);
        await store.startDownload({ kind: "playlist", id: "P1", name: "P" }, store.toSources("playlist", [track(4)]));
        expect(toast.error).toHaveBeenCalledWith("Downloaded 0 · skipped 0 · failed 1");
    });

    it("toasts the rejection of DownloadTracks and does not track the batch", async () => {
        const { api, toast, store } = await load();
        vi.mocked(api.DownloadTracks).mockRejectedValue("FFmpeg is required");

        await store.startDownload({ kind: "album", id: "A1", name: "Album" }, store.toSources("album", [track(1)]));

        expect(toast.error).toHaveBeenCalledWith("FFmpeg is required");
        expect(toast.success).not.toHaveBeenCalled();
        expect(vi.mocked(api.GetDownloadState)).not.toHaveBeenCalled();
    });

    it("does not start a collection without an id", async () => {
        const { api, store } = await load();

        await store.startDownload({ kind: "track", id: "", name: "x" }, store.toSources("track", [track(1)]));

        expect(api.DownloadTracks).not.toHaveBeenCalled();
    });

    it("keeps polling after a failed refresh and still reports the summary", async () => {
        const { api, toast, store } = await load();
        vi.mocked(api.GetDownloadState).mockRejectedValueOnce("offline");
        state(api, [item("B01", "completed")]);

        await store.startDownload({ kind: "album", id: "A1", name: "Album" }, store.toSources("album", [track(1)]));
        expect(toast.success).not.toHaveBeenCalled();
        await vi.advanceTimersByTimeAsync(600);

        expect(toast.success).toHaveBeenCalledWith("Downloaded 1 · skipped 0 · failed 0");
    });

    it("reports a re-run whose items are all skipped even though the data did not change", async () => {
        const { api, toast, store } = await load();
        state(api, [item("B01", "skipped")]);
        await store.refreshDownloads();

        await store.startDownload({ kind: "album", id: "A1", name: "Album" }, store.toSources("album", [track(1)]));

        expect(toast.success).toHaveBeenCalledWith("Downloaded 0 · skipped 1 · failed 0");
    });

    it("counts only the tracks of the current batch, not older runs in the queue", async () => {
        const { api, toast, store } = await load();
        const old = Array.from({ length: 10 }, (_, n) => item(`B0${n + 1}`, "completed"));
        state(api, old);
        await store.refreshDownloads();

        state(api, [item("B01", "queued"), ...old.slice(1)]);
        await store.startDownload({ kind: "album", id: "A1", name: "Album" }, store.toSources("album", [track(1)]));
        expect(store.batchProgress(store.getSnapshot(), "album:A1")).toEqual({ done: 0, total: 1, failed: 0, review: 0, active: true });
        state(api, old);
        await vi.advanceTimersByTimeAsync(600);

        expect(toast.success).toHaveBeenCalledTimes(1);
        expect(toast.success).toHaveBeenCalledWith("Downloaded 1 · skipped 0 · failed 0");
    });

    it("computes batch progress and stops only that queue's active items", async () => {
        const { api, store } = await load();
        const items = [item("a", "completed"), item("b", "skipped"), item("c", "failed"), item("d", "cancelled"), item("e", "downloading"), item("f", "queued"), item("g", "queued", "playlist:P"), item("h", "not_found"), item("i", "ignored")];
        state(api, items);
        await store.refreshDownloads();

        expect(store.batchProgress(store.getSnapshot(), "album:A1")).toEqual({ done: 4, total: 8, failed: 2, review: 0, active: true });
        await store.stopQueue("album:A1");
        expect(vi.mocked(api.CancelDownload).mock.calls.map((c) => c[0])).toEqual(["e", "f"]);
    });
});

describe("downloads store with Spotify tracks and review", () => {
    it("sends service, album_id, duration_ms, explicit and image for a Spotify track", async () => {
        const { api, store } = await load();
        const spotify = { ...track(1), service: "spotify" as const, id: "S1", album_id: "AL1", duration_ms: 215000, is_explicit: true, images: "https://i/c.jpg" };

        await store.startDownload({ kind: "track", id: "S1", name: "Song 1" }, store.toSources("track", [spotify]));

        expect(vi.mocked(api.DownloadTracks).mock.calls[0][0].tracks[0]).toMatchObject({ id: "S1", service: "spotify", album_id: "AL1", duration_ms: 215000, explicit: true, image: "https://i/c.jpg" });
    });

    it("waits for matching items but not for review items, and counts the review ones", async () => {
        const { api, toast, store } = await load();
        state(api, [item("B01", "completed"), item("B02", "matching"), item("B03", "review")]);
        await store.startDownload({ kind: "album", id: "A1", name: "Album" }, store.toSources("album", [track(1), track(2), track(3)]));
        expect(toast.success).not.toHaveBeenCalled();
        expect(store.batchProgress(store.getSnapshot(), "album:A1")).toMatchObject({ review: 1, active: true });

        state(api, [item("B01", "completed"), item("B02", "completed"), item("B03", "review")]);
        await vi.advanceTimersByTimeAsync(600);

        expect(toast.success).toHaveBeenCalledTimes(1);
        expect(toast.success).toHaveBeenCalledWith("Downloaded 2 · skipped 0 · failed 0 · 1 awaiting a decision");
        expect(store.batchProgress(store.getSnapshot(), "album:A1")).toMatchObject({ review: 1, active: false });
    });

    it("counts ignored as skipped and reports not found, leaving zero extras out", async () => {
        const { api, toast, store } = await load();
        state(api, [item("B01", "completed"), item("B02", "ignored"), item("B03", "not_found"), item("B04", "not_found")]);

        await store.startDownload({ kind: "album", id: "A1", name: "Album" }, store.toSources("album", [track(1), track(2), track(3), track(4)]));

        expect(toast.success).toHaveBeenCalledWith("Downloaded 1 · skipped 1 · failed 0 · 2 not found");
    });

    it("calls the new actions and refreshes after each", async () => {
        const { api, store } = await load();
        for (const f of [api.ChooseCandidate, api.UseAmazonLink, api.SkipReview, api.ResumeQueue, api.ClearQueue, api.GuardWait, api.GuardContinue]) vi.mocked(f).mockResolvedValue();

        await store.chooseCandidate("S1", "B0X");
        await store.useAmazonLink("S1", "https://music.amazon.com/albums/B0?trackAsin=B0X");
        await store.skipReview("S1");
        await store.resumeQueue();
        await store.clearQueue();
        await store.guardWait("lucida", 60);
        await store.guardContinue("lucida");

        expect(api.ChooseCandidate).toHaveBeenCalledWith("S1", "B0X");
        expect(api.UseAmazonLink).toHaveBeenCalledWith("S1", "https://music.amazon.com/albums/B0?trackAsin=B0X");
        expect(api.SkipReview).toHaveBeenCalledWith("S1");
        expect(api.GuardWait).toHaveBeenCalledWith("lucida", 60);
        expect(api.GuardContinue).toHaveBeenCalledWith("lucida");
        expect(vi.mocked(api.GetDownloadState)).toHaveBeenCalledTimes(7);
    });

    it("sends the guard wait from the settings", async () => {
        const { settings } = await load();
        expect(settings.downloadOptions({ ...settings.DEFAULT_SETTINGS, rateGuardWait: 60 }).guard_wait).toBe(60);
        expect(settings.downloadOptions(settings.DEFAULT_SETTINGS).guard_wait).toBe(30);
    });

    it("sends the discography, then top tracks that are not on a listed album", () => {
        return load().then(({ store }) => {
            const mk = (id: string, album_id: string) => ({ ...track(1), id, album_id });
            const sent = store.artistTracks({
                track_list: [mk("a", "AL1"), mk("b", "AL2")],
                top_tracks: [mk("c", "AL1"), mk("d", "OTHER"), mk("e", "")],
                album_list: [{ id: "AL1" }, { id: "AL2" }],
            });
            expect(sent.map((x) => x.id)).toEqual(["a", "b", "d", "e"]);
        });
    });

    it("copes with an artist payload saved before top_tracks existed", async () => {
        const { store } = await load();
        expect(store.artistTracks({ track_list: [{ ...track(1), id: "a" }], album_list: [] }).map((x) => x.id)).toEqual(["a"]);
    });

    it("never returns a track id twice, keeping the first", async () => {
        const { store } = await load();
        const mk = (id: string, album_id: string, name = id) => ({ ...track(1), id, album_id, name });
        const sent = store.artistTracks({
            track_list: [mk("a", "AL1", "first"), mk("b", "AL1")],
            top_tracks: [mk("a", "", "second"), mk("c", "zz"), mk("c", "zz")],
            album_list: [{ id: "AL1" }],
        });
        expect(sent.map((x) => x.id)).toEqual(["a", "b", "c"]);
        expect(sent[0].name).toBe("first");
    });
});
