import { afterAll, beforeAll, beforeEach, describe, expect, it, vi } from "vitest";
import type { LoadedMetadata, MetadataResponse } from "@/types/api";

const hookState = vi.hoisted(() => {
    let state: unknown[] = [];
    let refs: Array<{ current: unknown }> = [];
    let stateIndex = 0;
    let refIndex = 0;

    return {
        reset() {
            state = [];
            refs = [];
            stateIndex = 0;
            refIndex = 0;
        },
        beginRender() {
            stateIndex = 0;
            refIndex = 0;
        },
        useState<T>(initial: T | (() => T)) {
            const index = stateIndex++;
            if (index >= state.length) {
                state[index] = typeof initial === "function"
                    ? (initial as () => T)()
                    : initial;
            }
            const setState = (next: T | ((current: T) => T)) => {
                state[index] = typeof next === "function"
                    ? (next as (current: T) => T)(state[index] as T)
                    : next;
            };
            return [state[index] as T, setState] as const;
        },
        useRef<T>(initial: T) {
            const index = refIndex++;
            if (index >= refs.length) {
                refs[index] = { current: initial };
            }
            return refs[index] as { current: T };
        },
    };
});

const doubles = vi.hoisted(() => ({
    fetchMetadata: vi.fn(),
    addFetchHistory: vi.fn(),
    toastSuccess: vi.fn(),
    toastError: vi.fn(),
}));

vi.mock("react", async (importOriginal) => ({
    ...await importOriginal<typeof import("react")>(),
    useState: hookState.useState,
    useRef: hookState.useRef,
}));
vi.mock("@/lib/api", () => ({ fetchMetadata: doubles.fetchMetadata }));
vi.mock("@/lib/toast-with-sound", () => ({
    toastWithSound: { success: doubles.toastSuccess, error: doubles.toastError },
}));
vi.mock("@/lib/logger", () => ({
    logger: {
        info: vi.fn(),
        debug: vi.fn(),
        success: vi.fn(),
        warning: vi.fn(),
        error: vi.fn(),
    },
}));
vi.mock("../../wailsjs/go/main/App", () => ({ AddFetchHistory: doubles.addFetchHistory }));

let useMetadata: typeof import("./useMetadata")["useMetadata"];

beforeAll(async () => {
    vi.stubGlobal("window", { navigator: { platform: "Win32" } });
    ({ useMetadata } = await import("./useMetadata"));
});

afterAll(() => vi.unstubAllGlobals());

beforeEach(() => {
    hookState.reset();
    doubles.fetchMetadata.mockReset();
    doubles.addFetchHistory.mockReset().mockResolvedValue(undefined);
    doubles.toastSuccess.mockReset();
    doubles.toastError.mockReset();
});

function playlistMetadata(title: string): MetadataResponse {
    return {
        service: "spotify",
        playlist_info: {
            tracks: { total: 0 },
            followers: { total: 0 },
            owner: { display_name: "Owner", name: title, images: "" },
        },
        track_list: [],
    };
}

function albumMetadata(name: string): MetadataResponse {
    return {
        service: "spotify",
        album_info: {
            total_tracks: 0,
            name,
            release_date: "2026",
            artists: "Artist",
            images: "",
        },
        track_list: [],
    };
}

function deferred<T>() {
    let resolve!: (value: T) => void;
    let reject!: (reason: unknown) => void;
    const promise = new Promise<T>((resolvePromise, rejectPromise) => {
        resolve = resolvePromise;
        reject = rejectPromise;
    });
    return { promise, resolve, reject };
}

function MetadataHookHarness() {
    hookState.beginRender();
    return useMetadata();
}

describe("useMetadata async source identity", () => {
    const urlA = "https://open.spotify.com/playlist/a";
    const urlB = "https://open.spotify.com/playlist/b";

    it("keeps B when request A resolves after request B", async () => {
        const requestAResult = deferred<MetadataResponse>();
        const requestBResult = deferred<MetadataResponse>();
        const metadataA = playlistMetadata("Collection A");
        const metadataB = playlistMetadata("Collection B");
        doubles.fetchMetadata.mockImplementation((url: string) =>
            url === urlA ? requestAResult.promise : requestBResult.promise);

        let hook = MetadataHookHarness();
        const requestA = hook.handleFetchMetadata(urlA);
        hook = MetadataHookHarness();
        const requestB = hook.handleFetchMetadata(urlB);
        requestBResult.resolve(metadataB);
        await requestB;

        hook = MetadataHookHarness();
        expect(hook.loadedMetadata).toEqual({ data: metadataB, sourceUrl: urlB });

        requestAResult.resolve(metadataA);
        await requestA;
        hook = MetadataHookHarness();
        expect(hook.loadedMetadata).toEqual({ data: metadataB, sourceUrl: urlB });
        expect(hook.loading).toBe(false);
    });

    it("keeps cached history B when request A completes later", async () => {
        const requestAResult = deferred<MetadataResponse>();
        const metadataA = playlistMetadata("Collection A");
        const metadataB = playlistMetadata("Cached B");
        doubles.fetchMetadata.mockReturnValue(requestAResult.promise);

        let hook = MetadataHookHarness();
        const requestA = hook.handleFetchMetadata(urlA);
        hook.loadFromCache(JSON.stringify(metadataB), urlB);

        hook = MetadataHookHarness();
        expect(hook.loadedMetadata).toEqual({ data: metadataB, sourceUrl: urlB });
        expect(hook.loading).toBe(false);

        requestAResult.resolve(metadataA);
        await requestA;
        hook = MetadataHookHarness();
        expect(hook.loadedMetadata).toEqual({ data: metadataB, sourceUrl: urlB });
    });

    it("does not repopulate metadata after reset while a request is pending", async () => {
        const requestAResult = deferred<MetadataResponse>();
        doubles.fetchMetadata.mockReturnValue(requestAResult.promise);

        let hook = MetadataHookHarness();
        const requestA = hook.handleFetchMetadata(urlA);
        hook.resetMetadata();

        hook = MetadataHookHarness();
        expect(hook.loadedMetadata).toBeNull();
        expect(hook.loading).toBe(false);

        requestAResult.resolve(playlistMetadata("Collection A"));
        await requestA;
        hook = MetadataHookHarness();
        expect(hook.loadedMetadata).toBeNull();
    });

    it("ignores loading and error updates from a stale rejected request", async () => {
        const requestAResult = deferred<MetadataResponse>();
        const requestBResult = deferred<MetadataResponse>();
        doubles.fetchMetadata.mockImplementation((url: string) =>
            url === urlA ? requestAResult.promise : requestBResult.promise);

        let hook = MetadataHookHarness();
        const requestA = hook.handleFetchMetadata(urlA);
        hook = MetadataHookHarness();
        const requestB = hook.handleFetchMetadata(urlB);
        requestAResult.reject(new Error("stale A failed"));
        await requestA;
        const afterStaleFailure = MetadataHookHarness();

        requestBResult.resolve(playlistMetadata("Collection B"));
        await requestB;
        hook = MetadataHookHarness();
        expect(afterStaleFailure.loading).toBe(true);
        expect(doubles.toastError).not.toHaveBeenCalled();
        expect(hook.loadedMetadata?.sourceUrl).toBe(urlB);
    });

    it("retains the prior successful pair when the current sequential request fails", async () => {
        const metadataA = playlistMetadata("Collection A");
        doubles.fetchMetadata.mockRejectedValue(new Error("B failed"));

        let hook = MetadataHookHarness();
        hook.loadFromCache(JSON.stringify(metadataA), urlA);
        hook = MetadataHookHarness();
        await hook.handleFetchMetadata(urlB);
        hook = MetadataHookHarness();

        expect(hook.loadedMetadata).toEqual({ data: metadataA, sourceUrl: urlA });
        expect(hook.loading).toBe(false);
        expect(doubles.toastError).toHaveBeenCalledWith("B failed");
    });

    it("shows the text of a bare-string rejection from Go", async () => {
        const regionBlock = "Spotify is not available from your network region (HTTP 451). Turn on a VPN or set a proxy in Settings.";
        doubles.fetchMetadata.mockRejectedValue(regionBlock);

        const hook = MetadataHookHarness();
        await hook.handleFetchMetadata(urlA);

        expect(doubles.toastError).toHaveBeenCalledWith(regionBlock);
    });

    it("stores the canonical link in history for a pasted spotify: URI", async () => {
        const canonical = "https://open.spotify.com/album/6N9PS4QXF1D0OWPk0Sxtb4";
        const data = albumMetadata("A");
        if ("album_info" in data) data.album_info.external_urls = canonical;
        doubles.fetchMetadata.mockResolvedValue(data);

        const hook = MetadataHookHarness();
        await hook.handleFetchMetadata("spotify:album:6N9PS4QXF1D0OWPk0Sxtb4");

        expect(doubles.addFetchHistory).toHaveBeenCalledWith(expect.objectContaining({ url: canonical, type: "album" }));
    });

    const view = { searchQuery: "", sortBy: "default", page: 1 };

    it("retains the prior pair and the chain when a chain request fails", async () => {
        const metadataA = playlistMetadata("Collection A");
        doubles.fetchMetadata.mockRejectedValue(new Error("album B failed"));

        let hook = MetadataHookHarness();
        hook.loadFromCache(JSON.stringify(metadataA), urlA);
        hook = MetadataHookHarness();
        const fetched = await hook.handleFetchMetadata("https://open.spotify.com/album/b", view);
        hook = MetadataHookHarness();

        expect(fetched).toBeUndefined();
        expect(hook.loadedMetadata).toEqual({ data: metadataA, sourceUrl: urlA });
        expect(hook.canGoBack).toBe(false);
        expect(hook.loading).toBe(false);
        expect(doubles.toastError).toHaveBeenCalledWith("album B failed");
    });

    it("pushes on a chain fetch, goes back from memory with its view, and a new open drops the chain", async () => {
        const albumUrl = "https://open.spotify.com/album/b";
        const metadataA = playlistMetadata("Collection A");
        const metadataB = albumMetadata("Album B");
        doubles.fetchMetadata.mockResolvedValue(metadataB);

        let hook = MetadataHookHarness();
        hook.loadFromCache(JSON.stringify(metadataA), urlA);
        hook = MetadataHookHarness();
        expect(await hook.handleFetchMetadata(albumUrl, { ...view, page: 2 })).toBe(albumUrl);
        hook = MetadataHookHarness();
        expect(hook.loadedMetadata).toEqual({ data: metadataB, sourceUrl: albumUrl });
        expect(hook.canGoBack).toBe(true);
        expect(doubles.addFetchHistory).toHaveBeenCalledTimes(1);

        expect(hook.goBack()).toEqual({ data: metadataA, sourceUrl: urlA, view: { ...view, page: 2 } });
        hook = MetadataHookHarness();
        expect(hook.loadedMetadata).toEqual({ data: metadataA, sourceUrl: urlA, view: { ...view, page: 2 } });
        expect(hook.canGoBack).toBe(false);
        expect(hook.goBack()).toBeNull();
        expect(doubles.fetchMetadata).toHaveBeenCalledTimes(1);
        expect(doubles.addFetchHistory).toHaveBeenCalledTimes(1);

        await hook.handleFetchMetadata(albumUrl, view);
        hook = MetadataHookHarness();
        await hook.handleFetchMetadata(urlB);
        hook = MetadataHookHarness();
        expect(hook.canGoBack).toBe(false);
    });

    it("disables Back while a fetch is loading", async () => {
        const pending = deferred<MetadataResponse>();
        doubles.fetchMetadata.mockResolvedValueOnce(albumMetadata("A")).mockResolvedValueOnce(albumMetadata("B")).mockReturnValueOnce(pending.promise);

        let hook = MetadataHookHarness();
        await hook.handleFetchMetadata("https://open.spotify.com/album/a");
        hook = MetadataHookHarness();
        await hook.handleFetchMetadata("https://open.spotify.com/album/b", view);
        hook = MetadataHookHarness();
        expect(hook.canGoBack).toBe(true);

        const chain = hook.handleFetchMetadata("https://open.spotify.com/album/c", view);
        hook = MetadataHookHarness();
        expect(hook.canGoBack).toBe(false);
        pending.resolve(albumMetadata("C"));
        await chain;
    });
});
