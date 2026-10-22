// @vitest-environment jsdom
import { renderToStaticMarkup } from "react-dom/server";
import { act } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { TrackMetadata } from "@/types/api";
import { GetPreviewURL } from "../../wailsjs/go/main/App";
import { stopPreview } from "@/hooks/usePreview";
import { button, mount } from "../test/render";
import { TrackList } from "./TrackList";

vi.mock("../../wailsjs/go/main/App", () => ({ GetPreviewURL: vi.fn() }));
vi.mock("@/lib/toast-with-sound", () => ({ toastWithSound: { error: vi.fn() } }));

function makeTrack(n: number, over: Partial<TrackMetadata> = {}): TrackMetadata {
    return {
        service: "spotify",
        id: `track-${n}`,
        name: `Track ${n}`,
        artists: "Artist",
        album_name: "Album",
        duration_ms: 180_000,
        images: "",
        release_date: "2026",
        track_number: n,
        external_urls: `https://open.spotify.com/track/track-${n}`,
        ...over,
    };
}

function render(tracks: TrackMetadata[], props: { searchQuery?: string; sortBy?: string; currentPage?: number; itemsPerPage?: number } = {}): string {
    return renderToStaticMarkup(<TrackList
        tracks={tracks}
        searchQuery={props.searchQuery ?? ""}
        sortBy={props.sortBy ?? "default"}
        currentPage={props.currentPage ?? 1}
        itemsPerPage={props.itemsPerPage ?? 50}
        onPageChange={vi.fn()}
    />);
}

const titles = (markup: string) => [...markup.matchAll(/Track \d+/g)].map((m) => m[0]);

describe("TrackList", () => {
    it("renders the row fields", () => {
        const markup = render([makeTrack(1, { artists: "Some Artist", album_name: "Some Album", is_explicit: true, plays: "1234567" })]);

        expect(markup).toContain("Track 1");
        expect(markup).toContain("Some Artist");
        expect(markup).toContain("Some Album");
        expect(markup).toContain("3:00");
        expect(markup).toContain('title="Explicit"');
        expect(markup).toContain((1234567).toLocaleString("en"));
    });

    it("shows the plays column only when some track has plays", () => {
        expect(render([makeTrack(1), makeTrack(2)])).not.toContain(">Plays<");
        expect(render([makeTrack(1), makeTrack(2, { plays: "5" })])).toContain(">Plays<");
    });

    it("filters by search query across title, artist and album", () => {
        const tracks = [makeTrack(1), makeTrack(2, { artists: "Needle" }), makeTrack(3, { album_name: "needle album" })];

        expect(titles(render(tracks, { searchQuery: "NEEDLE" }))).toEqual(["Track 2", "Track 3"]);
    });

    it("sorts by title descending and duration ascending", () => {
        const tracks = [makeTrack(1, { duration_ms: 300_000 }), makeTrack(2, { duration_ms: 100_000 }), makeTrack(3, { duration_ms: 200_000 })];

        expect(titles(render(tracks, { sortBy: "title-desc" }))).toEqual(["Track 3", "Track 2", "Track 1"]);
        expect(titles(render(tracks, { sortBy: "duration-asc" }))).toEqual(["Track 2", "Track 3", "Track 1"]);
    });

    it("paginates and only shows the requested page", () => {
        const tracks = [makeTrack(1), makeTrack(2), makeTrack(3)];

        const page2 = render(tracks, { currentPage: 2, itemsPerPage: 2 });

        expect(titles(page2)).toEqual(["Track 3"]);
        expect(page2).toContain('aria-label="Go to next page"');
        expect(render(tracks)).not.toContain('aria-label="Go to next page"');
    });

    it("labels the open link per service and keeps the preview out of the Actions cell", () => {
        const doc = (markup: string) => new DOMParser().parseFromString(markup, "text/html");
        const spotify = doc(render([makeTrack(1)]));
        const amazon = doc(render([makeTrack(1, { service: "amazon" })]));

        for (const [parsed, label] of [[spotify, "Open in Spotify"], [amazon, "Open in Amazon Music"]] as const) {
            const actions = parsed.querySelector("tbody tr td:last-child")!;
            expect([...actions.querySelectorAll("button")].map((b) => b.getAttribute("aria-label"))).toEqual([label]);
        }
        expect(spotify.querySelectorAll('button[aria-label="Play preview"]').length).toBe(1);
        expect(amazon.querySelectorAll('button[aria-label="Play preview"]').length).toBe(0);
    });

    it("makes each artist its own link", async () => {
        const onArtistClick = vi.fn();
        const artists_data = [
            { id: "a1", name: "One", external_urls: "u1" },
            { id: "a2", name: "Two", external_urls: "u2" },
        ];
        const view = await mount(<TrackList tracks={[makeTrack(1, { artists: "One, Two", artists_data })]} searchQuery="" sortBy="default" currentPage={1} itemsPerPage={50} onPageChange={vi.fn()} onArtistClick={onArtistClick} />);

        button(view.container, "One").click();
        button(view.container, "Two").click();

        expect(onArtistClick.mock.calls).toEqual([[artists_data[0]], [artists_data[1]]]);
        await view.unmount();
    });

    it("takes artist names from the artists string when artists_data has none", async () => {
        const onArtistClick = vi.fn();
        // The Go album/playlist builders send artists_data with empty names.
        const artists_data = [
            { id: "a1", name: "", external_urls: "u1" },
            { id: "a2", name: "", external_urls: "u2" },
        ];
        const view = await mount(<TrackList tracks={[makeTrack(1, { artists: "A, B", artists_data })]} searchQuery="" sortBy="default" currentPage={1} itemsPerPage={50} onPageChange={vi.fn()} onArtistClick={onArtistClick} />);

        expect(button(view.container, "A").textContent).toBe("A");
        button(view.container, "B").click();

        expect(onArtistClick.mock.calls).toEqual([[{ id: "a2", name: "B", external_urls: "u2" }]]);
        await view.unmount();
    });

    it("shows plain text when the artist names and artists_data disagree in count", async () => {
        const artists_data = [{ id: "a1", name: "", external_urls: "u1" }, { id: "a2", name: "", external_urls: "u2" }];
        const view = await mount(<TrackList tracks={[makeTrack(1, { artists: "A, B, C", artists_data })]} searchQuery="" sortBy="default" currentPage={1} itemsPerPage={50} onPageChange={vi.fn()} onArtistClick={vi.fn()} />);

        expect(view.container.textContent).toContain("A, B, C");
        expect(() => button(view.container, "A")).toThrow();
        await view.unmount();
    });

    it("shows no download controls", () => {
        const markup = render([makeTrack(1, { images: "https://img/1.jpg" })]);
        const doc = new DOMParser().parseFromString(markup, "text/html");

        expect(doc.querySelectorAll('[role="checkbox"]').length).toBe(0);
        const buttons = [...doc.querySelectorAll("button")].map((b) => `${b.getAttribute("aria-label") ?? ""} ${b.textContent ?? ""}`);
        expect(buttons.filter((name) => /download/i.test(name))).toEqual([]);
    });
});

describe("TrackList downloads", () => {
    const base = { searchQuery: "", sortBy: "default", currentPage: 1, itemsPerPage: 50, onPageChange: vi.fn() };
    const item = (status: string, over: Record<string, string> = {}) => ({ id: "track-1", queue_id: "album:A", track: {}, status, message: "", path: "", error: "", ...over }) as never;
    const rowButtons = (container: HTMLElement) => [...container.querySelectorAll("tbody td:last-child button")] as HTMLButtonElement[];
    const downloadButton = (container: HTMLElement) => container.querySelector('tbody button[aria-label="Download"]') as HTMLButtonElement;

    it("calls onDownloadTrack with the index in the full list, also on page 2", async () => {
        const onDownloadTrack = vi.fn();
        const tracks = [1, 2, 3].map((n) => makeTrack(n, { service: "amazon" }));
        const view = await mount(<TrackList {...base} tracks={tracks} currentPage={2} itemsPerPage={2} canDownload onDownloadTrack={onDownloadTrack}/>);

        expect(downloadButton(view.container).disabled).toBe(false);
        downloadButton(view.container).click();

        expect(onDownloadTrack).toHaveBeenCalledWith(tracks[2], 2);
        await view.unmount();
    });

    it("enables the button for a Spotify track", async () => {
        const onDownloadTrack = vi.fn();
        const view = await mount(<TrackList {...base} tracks={[makeTrack(1)]} canDownload onDownloadTrack={onDownloadTrack}/>);

        expect(downloadButton(view.container).disabled).toBe(false);
        downloadButton(view.container).click();
        expect(onDownloadTrack).toHaveBeenCalledWith(expect.objectContaining({ service: "spotify" }), 0);
        await view.unmount();
    });

    it("disables the button without a download target and no longer blames Spotify", async () => {
        const view = await mount(<TrackList {...base} tracks={[makeTrack(1)]} canDownload={false} onDownloadTrack={vi.fn()}/>);

        expect(downloadButton(view.container).disabled).toBe(true);
        expect(downloadButton(view.container).closest("span")!.getAttribute("aria-description")).toBe("Download");
        await view.unmount();
    });

    it("retries a failed item", async () => {
        const onRetryDownload = vi.fn();
        const view = await mount(<TrackList {...base} tracks={[makeTrack(1, { service: "amazon" })]} canDownload onDownloadTrack={vi.fn()} onRetryDownload={onRetryDownload} downloadItems={new Map([["track-1", item("failed", { error: "boom" })]])}/>);
        const retry = rowButtons(view.container).find((b) => !b.getAttribute("aria-label")?.startsWith("Open"))!;

        retry.click();

        expect(onRetryDownload).toHaveBeenCalledWith("track-1");
        expect(retry.closest("span")!.getAttribute("aria-description")).toBe("boom");
        await view.unmount();
    });

    it("shows a disabled spinner while downloading and a check once completed", async () => {
        const onDownloadTrack = vi.fn();
        const props = { ...base, tracks: [makeTrack(1, { service: "amazon" })], canDownload: true, onDownloadTrack };
        const view = await mount(<TrackList {...props} downloadItems={new Map([["track-1", item("downloading", { message: "Converting" })]])}/>);
        const own = () => rowButtons(view.container).filter((b) => !b.getAttribute("aria-label")?.startsWith("Open"));

        expect(own()[0].disabled).toBe(true);
        expect(own()[0].closest("span")!.getAttribute("aria-description")).toBe("Converting");
        await view.render(<TrackList {...props} downloadItems={new Map([["track-1", item("completed", { path: "C:/music/a.flac" })]])}/>);
        expect(own()).toEqual([]);
        expect(view.container.querySelector('tbody span[aria-description="C:/music/a.flac"]')).not.toBeNull();
        expect(onDownloadTrack).not.toHaveBeenCalled();
        await view.unmount();
    });
});

describe("TrackList preview", () => {
    const pause = vi.fn();
    const listeners: Record<string, () => void> = {};
    beforeEach(() => {
        vi.resetAllMocks();
        pause.mockClear();
        vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
        vi.stubGlobal("Audio", class {
            addEventListener(name: string, fn: () => void) { listeners[name] = fn; }
            pause = pause;
            currentTime = 0;
            play() { return Promise.resolve(); }
        });
    });
    afterEach(() => { stopPreview(); vi.unstubAllGlobals(); });

    it("plays from the row cover and stops on a second click", async () => {
        vi.mocked(GetPreviewURL).mockResolvedValue("url1");
        const view = await mount(<TrackList tracks={[makeTrack(1)]} searchQuery="" sortBy="default" currentPage={1} itemsPerPage={50} onPageChange={vi.fn()} />);
        const cover = () => view.container.querySelector("tbody button[aria-label$=\" preview\"]") as HTMLButtonElement;

        await act(async () => cover().click());
        expect(GetPreviewURL).toHaveBeenCalledWith("track-1");
        await act(async () => listeners.loadeddata());
        expect(cover().getAttribute("aria-label")).toBe("Stop preview");

        await act(async () => cover().click());
        expect(pause).toHaveBeenCalledTimes(1);
        expect(cover().getAttribute("aria-label")).toBe("Play preview");
        await view.unmount();
    });
});
