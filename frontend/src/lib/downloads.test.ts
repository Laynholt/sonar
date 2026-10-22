// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { TrackMetadata } from "@/types/api";
import type { DownloadGuard, DownloadItem } from "./downloads";

vi.mock("../../wailsjs/go/main/App", () => ({
    DownloadTracks: vi.fn(), GetDownloadQueue: vi.fn(), GetDownloadGuard: vi.fn(),
    CancelDownload: vi.fn(), CancelAllDownloads: vi.fn(), RetryDownload: vi.fn(),
    RetryFailedDownloads: vi.fn(), ClearFinishedDownloads: vi.fn(), GuardWait: vi.fn(), GuardContinue: vi.fn(),
    LoadSettings: vi.fn(), SaveSettings: vi.fn(),
}));
vi.mock("@/lib/toast-with-sound", () => ({ toastWithSound: { success: vi.fn(), warning: vi.fn(), error: vi.fn() } }));

const idle = { site: "", count: 0, asking: false, paused_until: 0 } as DownloadGuard;
const track = (n: number): TrackMetadata => ({
    service: "amazon", id: `B0${n}`, name: `Song ${n}`, artists: "Artist", album_name: "Album", album_artist: "Album Artist",
    duration_ms: 1000, images: "", release_date: "2020", track_number: n + 4, disc_number: 2, external_urls: "",
});
const item = (id: string, status: string, queue_id = "album:A1") => ({ id, queue_id, track: {}, status, message: "", path: "", error: "" }) as DownloadItem;

async function load() {
    vi.resetModules();
    const api = await import("../../wailsjs/go/main/App");
    vi.mocked(api.DownloadTracks).mockResolvedValue();
    vi.mocked(api.GetDownloadQueue).mockResolvedValue([]);
    vi.mocked(api.GetDownloadGuard).mockResolvedValue(idle);
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
        expect(req.tracks[0]).toEqual({ id: "B01", title: "Song 1", artist: "Artist", album: "Album", album_artist: "Album Artist", release_date: "2020", track_number: 5, disc_number: 2, position: 5 });
        expect(req.tracks.map((t) => t.position)).toEqual([5, 6]);
    });

    it("numbers playlist and track positions by index + 1", async () => {
        const { store } = await load();

        expect(store.toSources("playlist", [track(1), track(2), track(3)]).map((s) => s.position)).toEqual([1, 2, 3]);
        expect(store.toSource("track", track(9), 6).position).toBe(7);
        expect(store.toSource("artist", track(9), 0).position).toBe(13);
    });

    it("polls while something is active, then stops", async () => {
        const { api, store } = await load();
        vi.mocked(api.GetDownloadQueue).mockResolvedValue([item("B01", "downloading")]);
        await store.startDownload({ kind: "album", id: "A1", name: "Album" }, store.toSources("album", [track(1)]));
        const callsAfterStart = vi.mocked(api.GetDownloadQueue).mock.calls.length;

        await vi.advanceTimersByTimeAsync(1600);
        expect(vi.mocked(api.GetDownloadQueue).mock.calls.length).toBe(callsAfterStart + 3);

        vi.mocked(api.GetDownloadQueue).mockResolvedValue([item("B01", "completed")]);
        await vi.advanceTimersByTimeAsync(600);
        const settled = vi.mocked(api.GetDownloadQueue).mock.calls.length;
        await vi.advanceTimersByTimeAsync(5000);
        expect(vi.mocked(api.GetDownloadQueue).mock.calls.length).toBe(settled);
    });

    it("keeps polling during a guard pause and never runs two loops", async () => {
        const { api, store } = await load();
        vi.mocked(api.GetDownloadGuard).mockResolvedValue({ site: "lucida", count: 50, asking: false, paused_until: Date.now() + 60_000 } as DownloadGuard);
        await Promise.all([store.refreshDownloads(), store.refreshDownloads(), store.refreshDownloads()]);
        const before = vi.mocked(api.GetDownloadQueue).mock.calls.length;

        await vi.advanceTimersByTimeAsync(1000);

        expect(vi.mocked(api.GetDownloadQueue).mock.calls.length - before).toBe(2);
        expect(store.getGuard().paused_until).toBeGreaterThan(0);
    });

    it("reflects the guard and keeps snapshots stable until the data changes", async () => {
        const { api, store } = await load();
        const guard = { site: "lucida", count: 50, asking: true, paused_until: 0 } as DownloadGuard;
        vi.mocked(api.GetDownloadGuard).mockResolvedValue(guard);
        const listener = vi.fn();
        store.subscribe(listener);

        await store.refreshDownloads();
        const items = store.getSnapshot();
        const g = store.getGuard();
        await store.refreshDownloads();

        expect(g).toEqual(guard);
        expect(store.getSnapshot()).toBe(items);
        expect(store.getGuard()).toBe(g);
        expect(listener).toHaveBeenCalledTimes(1);
    });

    it("shows one summary toast when the batch finishes", async () => {
        const { api, toast, store } = await load();
        vi.mocked(api.GetDownloadQueue).mockResolvedValue([item("B01", "downloading"), item("B02", "queued"), item("B03", "queued")]);
        await store.startDownload({ kind: "album", id: "A1", name: "Album" }, store.toSources("album", [track(1), track(2), track(3)]));
        expect(toast.success).not.toHaveBeenCalled();

        vi.mocked(api.GetDownloadQueue).mockResolvedValue([item("B01", "completed"), item("B02", "completed"), item("B03", "skipped")]);
        await vi.advanceTimersByTimeAsync(600);
        await vi.advanceTimersByTimeAsync(3000);

        expect(toast.success).toHaveBeenCalledTimes(1);
        expect(toast.success).toHaveBeenCalledWith("Downloaded 2 · skipped 1 · failed 0");
    });

    it("warns on partial failure and errors when nothing downloaded", async () => {
        const { api, toast, store } = await load();
        vi.mocked(api.GetDownloadQueue).mockResolvedValue([item("B01", "completed"), item("B02", "failed"), item("B03", "cancelled")]);
        await store.startDownload({ kind: "album", id: "A1", name: "Album" }, []);
        expect(toast.warning).toHaveBeenCalledWith("Downloaded 1 · skipped 0 · failed 2");

        vi.mocked(api.GetDownloadQueue).mockResolvedValue([item("B04", "failed", "playlist:P1")]);
        await store.startDownload({ kind: "playlist", id: "P1", name: "P" }, []);
        expect(toast.error).toHaveBeenCalledWith("Downloaded 0 · skipped 0 · failed 1");
    });

    it("toasts the rejection of DownloadTracks and does not track the batch", async () => {
        const { api, toast, store } = await load();
        vi.mocked(api.DownloadTracks).mockRejectedValue("FFmpeg is required");

        await store.startDownload({ kind: "album", id: "A1", name: "Album" }, []);

        expect(toast.error).toHaveBeenCalledWith("FFmpeg is required");
        expect(toast.success).not.toHaveBeenCalled();
    });

    it("computes batch progress and stops only that queue's active items", async () => {
        const { api, store } = await load();
        const items = [item("a", "completed"), item("b", "skipped"), item("c", "failed"), item("d", "cancelled"), item("e", "downloading"), item("f", "queued"), item("g", "queued", "playlist:P")];
        vi.mocked(api.GetDownloadQueue).mockResolvedValue(items);
        await store.refreshDownloads();

        expect(store.batchProgress(store.getSnapshot(), "album:A1")).toEqual({ done: 2, total: 6, failed: 2, active: true });
        await store.stopQueue("album:A1");
        expect(vi.mocked(api.CancelDownload).mock.calls.map((c) => c[0])).toEqual(["e", "f"]);
    });
});
