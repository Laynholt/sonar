// @vitest-environment jsdom
import { act } from "react";
import { createRoot } from "react-dom/client";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { MetadataResponse } from "@/types/api";

const playlist: MetadataResponse = {
    service: "spotify",
    playlist_info: { tracks: { total: 60 }, followers: { total: 0 }, owner: { display_name: "Owner", name: "Mix", images: "" } },
    track_list: Array.from({ length: 60 }, (_, i) => ({
        service: "spotify" as const, id: `t${i + 1}`, name: `Song ${i + 1}`, artists: "Artist", album_name: "Album",
        duration_ms: 1000, images: "", release_date: "2026", track_number: i + 1, external_urls: "",
    })),
};

vi.mock("@/lib/api", () => ({ fetchMetadata: vi.fn().mockResolvedValue(playlist) }));
vi.mock("../wailsjs/go/main/App", () => ({ AddFetchHistory: vi.fn().mockResolvedValue(undefined), LoadSettings: vi.fn().mockResolvedValue({}), SaveSettings: vi.fn() }));
vi.mock("../wailsjs/runtime/runtime", () => ({ Environment: vi.fn().mockResolvedValue({ platform: "windows" }), WindowMinimise: vi.fn(), WindowToggleMaximise: vi.fn(), Quit: vi.fn(), BrowserOpenURL: vi.fn() }));
vi.mock("@/lib/toast-with-sound", () => ({ toastWithSound: { success: vi.fn(), error: vi.fn(), info: vi.fn(), warning: vi.fn() } }));

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
let container: HTMLDivElement | undefined;
afterEach(() => { container?.remove(); });

function setValue(el: HTMLInputElement, value: string) {
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, "value")!.set!.call(el, value);
    el.dispatchEvent(new Event("input", { bubbles: true }));
}

describe("App main page", () => {
    it("returns to page 1 when a search is typed on a later page", async () => {
        Object.defineProperty(window, "matchMedia", { value: () => ({ matches: false, addEventListener: vi.fn(), removeEventListener: vi.fn() }) });
        const { default: App } = await import("./App");
        container = document.createElement("div");
        document.body.append(container);
        const root = createRoot(container);
        await act(async () => { root.render(<App />); });

        await act(async () => { setValue(container!.querySelector("input")!, "https://open.spotify.com/playlist/x"); });
        await act(async () => { container!.querySelector("form")!.requestSubmit(); });
        await act(async () => { [...container!.querySelectorAll("a")].find((a) => a.textContent === "2")!.click(); });
        expect(container.textContent).toContain("Song 51");

        await act(async () => { setValue(container!.querySelector('input[placeholder="Search tracks..."]')!, "Song 5"); });

        // 11 matches fit on one page; without the reset, page 2 would be empty.
        expect(container.textContent).toContain("Song 50");
    });
});
