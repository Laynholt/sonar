// @vitest-environment jsdom
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from "vitest";
import type { MetadataResponse } from "@/types/api";
import { toastWithSound as toast } from "@/lib/toast-with-sound";
import { stopPreview } from "@/hooks/usePreview";
import { fetchMetadata } from "@/lib/api";
import { getLanguage, setLanguage } from "@/lib/i18n";
import { getSettings, saveSettings } from "@/lib/settings";
import { AddFetchHistory, DownloadTracks, GetFetchHistory, GetFetchHistoryData, SaveSettings } from "../wailsjs/go/main/App";

const albumUrl = "https://open.spotify.com/album/al";
const playlistUrl = "https://open.spotify.com/playlist/x";
const playlist: MetadataResponse = {
    service: "spotify",
    playlist_info: { tracks: { total: 60 }, followers: { total: 0 }, owner: { display_name: "Owner", name: "Mix", images: "" } },
    track_list: Array.from({ length: 60 }, (_, i) => ({
        service: "spotify" as const, id: `t${i + 1}`, name: `Song ${i + 1}`, artists: "Artist", album_name: "Album",
        duration_ms: 1000, images: "", release_date: "2026", track_number: i + 1, external_urls: "",
        album_id: "al", album_url: albumUrl,
    })),
};
const album: MetadataResponse = {
    service: "spotify",
    album_info: { total_tracks: 1, name: "Chain Album", release_date: "2026", artists: "Artist", images: "", external_urls: albumUrl },
    track_list: [],
};

vi.mock("@/components/ui/select", () => import("./test/nativeSelect"));
vi.mock("@/lib/api", () => ({ fetchMetadata: vi.fn() }));
vi.mock("@/hooks/usePreview", async (original) => ({ ...await original<typeof import("@/hooks/usePreview")>(), stopPreview: vi.fn() }));
vi.mock("../wailsjs/go/main/App", () => ({ DownloadTracks: vi.fn().mockResolvedValue(undefined), GetDownloadState: vi.fn().mockResolvedValue({ items: [], sites: [], held: false }), GetPendingQueue: vi.fn().mockResolvedValue({ tracks: 0, review: 0, collections: [] }), AddFetchHistory: vi.fn().mockResolvedValue(undefined), GetFetchHistory: vi.fn(), GetFetchHistoryData: vi.fn(), DeleteFetchHistoryItem: vi.fn(), SearchSpotify: vi.fn(), SearchSpotifyByType: vi.fn(), LoadSettings: vi.fn().mockResolvedValue({}), SaveSettings: vi.fn() }));
vi.mock("../wailsjs/runtime/runtime", () => ({ Environment: vi.fn().mockResolvedValue({ platform: "windows" }), WindowMinimise: vi.fn(), WindowToggleMaximise: vi.fn(), Quit: vi.fn(), BrowserOpenURL: vi.fn() }));
vi.mock("@/lib/toast-with-sound", () => ({ toastWithSound: { success: vi.fn(), error: vi.fn(), info: vi.fn(), warning: vi.fn() } }));

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
let container: HTMLDivElement | undefined;
let root: Root | undefined;
// App pulls in the whole UI; load it once up front so the first test is not charged for the transform.
beforeAll(async () => { await import("./App"); }, 30_000);
beforeEach(async () => {
    vi.mocked(GetFetchHistory).mockResolvedValue([]);
    vi.mocked(fetchMetadata).mockReset().mockImplementation(async (url) => (url === albumUrl ? album : playlist));
    vi.mocked(AddFetchHistory).mockClear();
    vi.mocked(stopPreview).mockClear();
    vi.mocked(toast.error).mockClear();
});
// Unmount so the window listeners of one test do not outlive it.
afterEach(async () => { await act(async () => root?.unmount()); container?.remove(); setLanguage("en"); });

async function mountApp() {
    Object.defineProperty(window, "matchMedia", { configurable: true, value: () => ({ matches: false, addEventListener: vi.fn(), removeEventListener: vi.fn() }) });
    const { default: App } = await import("./App");
    container = document.createElement("div");
    document.body.append(container);
    root = createRoot(container);
    await act(async () => { root!.render(<App />); });
}

function setValue(el: HTMLInputElement, value: string) {
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, "value")!.set!.call(el, value);
    el.dispatchEvent(new Event("input", { bubbles: true }));
}

describe("App main page", () => {
    it("enables the download buttons on a Spotify page", async () => {
        await mountApp();
        await act(async () => { setValue(container!.querySelector("#spotify-url")!, "https://open.spotify.com/playlist/x"); });
        await act(async () => { [...container!.querySelectorAll("button")].find((b) => b.textContent === "Fetch")!.click(); });

        const all = [...container!.querySelectorAll("button")].find((b) => b.textContent === "Download all")!;
        const row = container!.querySelector<HTMLButtonElement>('tbody button[aria-label="Download"]')!;
        expect([all.disabled, row.disabled]).toEqual([false, false]);
        expect(document.body.textContent).not.toContain("next update");
    });

    it("returns to page 1 when a search is typed on a later page", async () => {
        await mountApp();

        await act(async () => { setValue(container!.querySelector("#spotify-url")!, "https://open.spotify.com/playlist/x"); });
        await act(async () => { [...container!.querySelectorAll("button")].find((b) => b.textContent === "Fetch")!.click(); });
        await act(async () => { [...container!.querySelectorAll("a")].find((a) => a.textContent === "2")!.click(); });
        expect(container!.textContent).toContain("Song 51");

        await act(async () => { setValue(container!.querySelector('input[placeholder="Search tracks..."]')!, "Song 5"); });

        // 11 matches fit on one page; without the reset, page 2 would be empty.
        expect(container!.textContent).toContain("Song 50");
    });

    it("lists a saved fetch under Recent Fetches and reopens it without a network call", async () => {
        vi.mocked(GetFetchHistory).mockResolvedValue([{ id: "h1", url: "https://open.spotify.com/playlist/x", type: "playlist", name: "Mix", info: "60 tracks", image: "", data: "", timestamp: 1 }]);
        vi.mocked(GetFetchHistoryData).mockResolvedValue(JSON.stringify(playlist));
        await mountApp();
        expect(container!.textContent).toContain("Recent Fetch");

        await act(async () => { container!.querySelector<HTMLElement>("[title='Mix']")!.click(); });

        expect(GetFetchHistoryData).toHaveBeenCalledWith("h1");
        expect(container!.textContent).toContain("Song 1");
        expect(fetchMetadata).not.toHaveBeenCalled();
        expect((container!.querySelector("#spotify-url") as HTMLInputElement).value).toBe("https://open.spotify.com/playlist/x");
    });

    it("shows the history error and keeps the page usable when history is unavailable", async () => {
        vi.mocked(GetFetchHistory).mockRejectedValue("history is unavailable");
        await mountApp();
        expect(toast.error).toHaveBeenCalledWith("Could not load recent fetches", { description: "history is unavailable" });
        expect(container!.querySelector("#spotify-url")).not.toBeNull();
    });
});

describe("back button over the click chain", () => {
    const backButton = () => container!.querySelector<HTMLButtonElement>('button[aria-label="Back"]')!;
    const urlInput = () => container!.querySelector<HTMLInputElement>("#spotify-url")!;
    const clickText = async (selector: string, text: string) => {
        await act(async () => { [...container!.querySelectorAll<HTMLElement>(selector)].find((el) => el.textContent === text)!.click(); });
    };
    const openPlaylist = async () => {
        await mountApp();
        await act(async () => { setValue(urlInput(), playlistUrl); });
        await clickText("button", "Fetch");
    };
    const clickAlbum = () => clickText("button", "Album");
    const historyWrites = () => vi.mocked(AddFetchHistory).mock.calls.length;

    it("opens an album from the list without a dialog, records it, and Back returns from memory", async () => {
        await openPlaylist();
        expect(backButton().disabled).toBe(true);
        const writes = historyWrites();

        await clickAlbum();

        expect(fetchMetadata).toHaveBeenLastCalledWith(albumUrl);
        expect(container!.querySelector('[role="dialog"]')).toBeNull();
        expect(container!.textContent).toContain("Chain Album");
        expect(backButton().disabled).toBe(false);
        expect(historyWrites()).toBe(writes + 1);
        expect(vi.mocked(AddFetchHistory).mock.lastCall![0]).toMatchObject({ type: "album", url: albumUrl });

        vi.mocked(stopPreview).mockClear();
        await act(async () => { backButton().click(); });

        expect(container!.textContent).toContain("Song 1");
        expect(container!.textContent).not.toContain("Chain Album");
        expect(fetchMetadata).toHaveBeenCalledTimes(2);
        expect(historyWrites()).toBe(writes + 1);
        expect(urlInput().value).toBe(playlistUrl);
        expect(backButton().disabled).toBe(true);
        expect(stopPreview).toHaveBeenCalled();
    });

    it("restores the page and the search query", async () => {
        await openPlaylist();
        await act(async () => { setValue(container!.querySelector('input[placeholder="Search tracks..."]')!, "Song"); });
        await clickText("a", "2");
        expect(container!.textContent).toContain("Song 51");

        await clickAlbum();
        expect(container!.textContent).toContain("Chain Album");
        await act(async () => { backButton().click(); });

        expect(container!.querySelector<HTMLInputElement>('input[placeholder="Search tracks..."]')!.value).toBe("Song");
        expect(container!.textContent).toContain("Song 51");
    });

    it("leaves the playlist and Back state alone when the chain fetch fails", async () => {
        await openPlaylist();
        vi.mocked(fetchMetadata).mockRejectedValueOnce(new Error("offline"));

        await clickAlbum();

        expect(container!.textContent).not.toContain("Chain Album");
        expect(container!.textContent).toContain("Song 1");
        expect(backButton().disabled).toBe(true);
        expect(urlInput().value).toBe(playlistUrl);
    });

    it.each([
        ["Alt+ArrowLeft", () => window.dispatchEvent(new KeyboardEvent("keydown", { key: "ArrowLeft", altKey: true, cancelable: true }))],
        ["the mouse back button", () => window.dispatchEvent(new MouseEvent("mouseup", { button: 3, cancelable: true }))],
    ])("goes back on %s", async (_name, fire) => {
        await openPlaylist();
        await clickAlbum();
        expect(container!.textContent).toContain("Chain Album");

        await act(async () => { fire(); });

        expect(container!.textContent).toContain("Song 1");
        expect(backButton().disabled).toBe(true);
        // With nothing to go back to the shortcut does nothing.
        await act(async () => { fire(); });
        expect(container!.textContent).toContain("Song 1");
    });

    it("drops the chain on close and on a new Fetch", async () => {
        await openPlaylist();
        await clickAlbum();
        await act(async () => { container!.querySelector<HTMLElement>(".absolute.top-4.right-4 button")!.click(); });
        expect(container!.textContent).not.toContain("Chain Album");
        expect(backButton().disabled).toBe(true);

        await act(async () => { setValue(urlInput(), playlistUrl); });
        await clickText("button", "Fetch");
        await clickAlbum();
        expect(backButton().disabled).toBe(false);
        await act(async () => { setValue(urlInput(), "https://open.spotify.com/playlist/y"); });
        await clickText("button", "Fetch");
        expect(backButton().disabled).toBe(true);
    });

    it("stops the preview when the user leaves the main page", async () => {
        await openPlaylist();
        vi.mocked(stopPreview).mockClear();

        await act(async () => { container!.querySelector<HTMLElement>('button[aria-label="History"]')!.click(); });

        expect(stopPreview).toHaveBeenCalled();
    });
});

describe("artist tabs over the click chain", () => {
    const artistUrl = "https://open.spotify.com/artist/0123456789abcdefghijAB/discography/all";
    const artist: MetadataResponse = {
        service: "spotify",
        artist_info: { name: "Some Artist", images: "", followers: 1, external_urls: artistUrl, discography_type: "all", total_albums: 0 },
        album_list: [{ id: "al", name: "Album", album_type: "album", release_date: "2026", total_tracks: 1, artists: "Some Artist", images: "", external_urls: albumUrl }],
        top_tracks: [
            { service: "spotify", id: "top-on-album", name: "On album", artists: "Some Artist", album_name: "Album", duration_ms: 1000, images: "", release_date: "2026", track_number: 1, external_urls: "", album_id: "al" },
            { service: "spotify", id: "top-single", name: "Single", artists: "Some Artist", album_name: "Other", duration_ms: 1000, images: "", release_date: "2026", track_number: 1, external_urls: "", album_id: "zz" },
        ],
        track_list: [{ service: "spotify", id: "s1", name: "Song 1", artists: "Some Artist", album_name: "Album", duration_ms: 1000, images: "", release_date: "2026", track_number: 1, external_urls: "", album_id: "al", album_url: albumUrl }],
    };
    const searchBox = () => container!.querySelector<HTMLInputElement>('input[placeholder="Search tracks..."]');
    const click = (selector: string, text: string) => act(async () => { [...container!.querySelectorAll<HTMLElement>(selector)].find((el) => el.textContent === text)!.click(); });

    it("downloads the discography, then the top tracks that are not on a listed album", async () => {
        vi.mocked(fetchMetadata).mockImplementation(async () => artist);
        await mountApp();
        await act(async () => { setValue(container!.querySelector("#spotify-url")!, artistUrl); });
        await click("button", "Fetch");

        await act(async () => { [...container!.querySelectorAll("button")].find((b) => b.textContent === "Download all")!.click(); });

        const request = vi.mocked(DownloadTracks).mock.calls[0][0];
        expect(request.collection.kind).toBe("artist");
        expect(request.tracks.map((x) => x.id)).toEqual(["s1", "top-single"]);
    });

    it("brings Back to the Tracks tab with its query", async () => {
        vi.mocked(fetchMetadata).mockImplementation(async (url) => (url === albumUrl ? album : artist));
        await mountApp();
        await act(async () => { setValue(container!.querySelector("#spotify-url")!, artistUrl); });
        await click("button", "Fetch");
        expect(searchBox()).toBeNull();

        await click("button", "All Tracks");
        await act(async () => { setValue(searchBox()!, "Song"); });
        await click("button", "Album");
        expect(container!.textContent).toContain("Chain Album");
        await act(async () => { container!.querySelector<HTMLElement>('button[aria-label="Back"]')!.click(); });

        expect(searchBox()!.value).toBe("Song");
    });
});

describe("unsaved language", () => {
    it("reverts the previewed language on Discard Changes", async () => {
        localStorage.setItem("sonar-settings", JSON.stringify({ language: "en" }));
        await mountApp();
        await act(async () => { container!.querySelector<HTMLElement>('button[aria-label="Settings"]')!.click(); });
        const select = container!.querySelector("select")!;
        await act(async () => {
            select.value = "ru";
            select.dispatchEvent(new Event("change", { bubbles: true }));
        });
        expect(getLanguage()).toBe("ru");

        // The previewed language is already Russian, so the labels are too.
        await act(async () => { container!.querySelector<HTMLElement>('button[aria-label="История"]')!.click(); });
        await act(async () => { [...document.querySelectorAll("button")].find((b) => b.textContent === "Отбросить")!.click(); });

        expect(getLanguage()).toBe("en");
    });

    it("saves the changes and leaves on Save", async () => {
        localStorage.setItem("sonar-settings", JSON.stringify({ language: "en" }));
        await mountApp();
        await act(async () => { container!.querySelector<HTMLElement>('button[aria-label="Settings"]')!.click(); });
        const select = container!.querySelector("select")!;
        await act(async () => {
            select.value = "ru";
            select.dispatchEvent(new Event("change", { bubbles: true }));
        });
        await act(async () => { container!.querySelector<HTMLElement>('button[aria-label="История"]')!.click(); });
        const buttons = [...document.querySelectorAll("[role=dialog] button")].map((b) => b.textContent);
        expect(buttons).toEqual(["Отбросить", "Сохранить"]);
        await act(async () => { [...document.querySelectorAll("button")].find((b) => b.textContent === "Сохранить")!.click(); });

        expect(vi.mocked(SaveSettings).mock.lastCall![0]).toMatchObject({ language: "ru" });
        expect(getLanguage()).toBe("ru");
        expect(container!.querySelector("select")).toBeNull();
        await saveSettings({ ...getSettings(), language: "en" }); // later tests start in English
    });
});

describe("translation", () => {
    it("shows Russian sidebar labels after the language is switched", async () => {
        await mountApp();
        expect(container!.querySelector('button[aria-label="Home"]')).not.toBeNull();

        await act(async () => { setLanguage("ru"); });

        expect(container!.querySelector('button[aria-label="Главная"]')).not.toBeNull();
        expect(container!.querySelector('button[aria-label="Настройки"]')).not.toBeNull();
        expect(container!.querySelector('button[aria-label="Home"]')).toBeNull();
    });

    it("stores the bare track count for a new playlist fetch", async () => {
        await mountApp();
        await act(async () => { setValue(container!.querySelector("#spotify-url")!, playlistUrl); });
        await act(async () => { [...container!.querySelectorAll("button")].find((b) => b.textContent === "Fetch")!.click(); });

        expect(vi.mocked(AddFetchHistory).mock.lastCall![0]).toMatchObject({ type: "playlist", info: "60" });
    });
});
