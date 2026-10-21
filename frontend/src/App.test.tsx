// @vitest-environment jsdom
import { act } from "react";
import { createRoot } from "react-dom/client";
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from "vitest";
import type { MetadataResponse } from "@/types/api";
import { toastWithSound as toast } from "@/lib/toast-with-sound";
import { GetFetchHistory } from "../wailsjs/go/main/App";

const playlist: MetadataResponse = {
    service: "spotify",
    playlist_info: { tracks: { total: 60 }, followers: { total: 0 }, owner: { display_name: "Owner", name: "Mix", images: "" } },
    track_list: Array.from({ length: 60 }, (_, i) => ({
        service: "spotify" as const, id: `t${i + 1}`, name: `Song ${i + 1}`, artists: "Artist", album_name: "Album",
        duration_ms: 1000, images: "", release_date: "2026", track_number: i + 1, external_urls: "",
    })),
};

vi.mock("@/lib/api", () => ({ fetchMetadata: vi.fn().mockResolvedValue(playlist) }));
vi.mock("../wailsjs/go/main/App", () => ({ AddFetchHistory: vi.fn().mockResolvedValue(undefined), GetFetchHistory: vi.fn(), DeleteFetchHistoryItem: vi.fn(), SearchSpotify: vi.fn(), SearchSpotifyByType: vi.fn(), LoadSettings: vi.fn().mockResolvedValue({}), SaveSettings: vi.fn() }));
vi.mock("../wailsjs/runtime/runtime", () => ({ Environment: vi.fn().mockResolvedValue({ platform: "windows" }), WindowMinimise: vi.fn(), WindowToggleMaximise: vi.fn(), Quit: vi.fn(), BrowserOpenURL: vi.fn() }));
vi.mock("@/lib/toast-with-sound", () => ({ toastWithSound: { success: vi.fn(), error: vi.fn(), info: vi.fn(), warning: vi.fn() } }));

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
let container: HTMLDivElement | undefined;
// App pulls in the whole UI; load it once up front so the first test is not charged for the transform.
beforeAll(async () => { await import("./App"); }, 30_000);
beforeEach(async () => {
    vi.mocked(GetFetchHistory).mockResolvedValue([]);
    vi.mocked((await import("@/lib/api")).fetchMetadata).mockClear();
    vi.mocked(toast.error).mockClear();
});
afterEach(() => { container?.remove(); });

async function mountApp() {
    Object.defineProperty(window, "matchMedia", { configurable: true, value: () => ({ matches: false, addEventListener: vi.fn(), removeEventListener: vi.fn() }) });
    const { default: App } = await import("./App");
    container = document.createElement("div");
    document.body.append(container);
    await act(async () => { createRoot(container!).render(<App />); });
}

function setValue(el: HTMLInputElement, value: string) {
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, "value")!.set!.call(el, value);
    el.dispatchEvent(new Event("input", { bubbles: true }));
}

describe("App main page", () => {
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
        vi.mocked(GetFetchHistory).mockResolvedValue([{ id: "h1", url: "https://open.spotify.com/playlist/x", type: "playlist", name: "Mix", info: "60 tracks", image: "", data: JSON.stringify(playlist), timestamp: 1 }]);
        await mountApp();
        expect(container!.textContent).toContain("Recent Fetch");

        await act(async () => { container!.querySelector<HTMLElement>("[title='Mix']")!.click(); });

        expect(container!.textContent).toContain("Song 1");
        expect((await import("@/lib/api")).fetchMetadata).not.toHaveBeenCalled();
        expect((container!.querySelector("#spotify-url") as HTMLInputElement).value).toBe("https://open.spotify.com/playlist/x");
    });

    it("shows the history error and keeps the page usable when history is unavailable", async () => {
        vi.mocked(GetFetchHistory).mockRejectedValue("history is unavailable");
        await mountApp();
        expect(toast.error).toHaveBeenCalledWith("Could not load recent fetches", { description: "history is unavailable" });
        expect(container!.querySelector("#spotify-url")).not.toBeNull();
    });
});
