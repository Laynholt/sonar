// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import type { TrackMetadata } from "@/types/api";
import { mount, button } from "../test/render";
import { TrackInfo } from "./TrackInfo";

vi.mock("../../wailsjs/go/main/App", () => ({ GetPreviewURL: vi.fn() }));
vi.mock("../../wailsjs/runtime/runtime", () => ({ BrowserOpenURL: vi.fn() }));
vi.mock("@/lib/toast-with-sound", () => ({ toastWithSound: { error: vi.fn() } }));

const track: TrackMetadata & { album_name: string; release_date: string } = {
    service: "spotify",
    id: "t1",
    name: "Song",
    artists: "First, Second",
    artists_data: [
        { id: "a1", name: "First", external_urls: "https://open.spotify.com/artist/a1" },
        { id: "a2", name: "Second", external_urls: "https://open.spotify.com/artist/a2" },
    ],
    album_name: "Album",
    album_id: "al1",
    album_url: "https://open.spotify.com/album/al1",
    duration_ms: 185_000,
    images: "",
    release_date: "2026-01-02",
    track_number: 1,
    plays: "1234",
    copyright: "℗ Label",
    external_urls: "https://open.spotify.com/track/t1",
};

let view: Awaited<ReturnType<typeof mount>>;
afterEach(async () => { await view.unmount(); });

describe("TrackInfo", () => {
    it("shows labelled details", async () => {
        view = await mount(<TrackInfo track={track}/>);
        const text = view.container.textContent!;

        for (const part of ["Release Date", "2026-01-02", "Plays", (1234).toLocaleString("en"), "Duration", "3:05", "Copyright", "℗ Label"]) expect(text).toContain(part);
    });

    it("opens the clicked artist", async () => {
        const onArtistClick = vi.fn();
        view = await mount(<TrackInfo track={track} onArtistClick={onArtistClick}/>);

        button(view.container, "Second").click();

        expect(onArtistClick).toHaveBeenCalledWith({ id: "a2", name: "Second", external_urls: "https://open.spotify.com/artist/a2" });
    });

    it("opens the album", async () => {
        const onAlbumClick = vi.fn();
        view = await mount(<TrackInfo track={track} onAlbumClick={onAlbumClick}/>);

        button(view.container, "Album").click();

        expect(onAlbumClick).toHaveBeenCalledWith({ id: "al1", name: "Album", external_urls: "https://open.spotify.com/album/al1" });
    });

    it("renders the album as plain text without a link", async () => {
        view = await mount(<TrackInfo track={{ ...track, album_url: undefined }} onAlbumClick={vi.fn()}/>);

        expect(view.container.textContent).toContain("Album");
        expect(() => button(view.container, "Album")).toThrow();
    });

    it("offers a preview for Spotify tracks but not Amazon ones", async () => {
        view = await mount(<TrackInfo track={track}/>);
        expect(view.container.querySelectorAll('button[aria-label="Play preview"]').length).toBe(1);

        await view.render(<TrackInfo track={{ ...track, service: "amazon" }}/>);
        expect(view.container.querySelectorAll('button[aria-label="Play preview"]').length).toBe(0);
    });

    it("has an open-in-service button", async () => {
        view = await mount(<TrackInfo track={track}/>);

        expect(button(view.container, "Open in Spotify")).toBeTruthy();
    });
});
