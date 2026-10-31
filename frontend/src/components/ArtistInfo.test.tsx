// @vitest-environment jsdom
import { describe, expect, it, vi } from "vitest";
import { mount } from "../test/render";
import { ArtistInfo } from "./ArtistInfo";
import { TooltipProvider } from "./ui/tooltip";

vi.mock("../../wailsjs/go/main/App", () => ({ GetPreviewURL: vi.fn() }));
vi.mock("../../wailsjs/runtime/runtime", () => ({ BrowserOpenURL: vi.fn() }));
vi.mock("@/hooks/useSources", async () => (await import("../test/sources")).useSourcesMock);
vi.mock("@/lib/toast-with-sound", () => ({ toastWithSound: { error: vi.fn() } }));

const noop = vi.fn();
const album = { id: "al", name: "Album", images: "", release_date: "2020-01-01", album_type: "album", external_urls: "", total_tracks: 0 };

describe("ArtistInfo", () => {
    it("hides counts the service does not provide instead of showing 0", async () => {
        const view = await mount(<TooltipProvider><ArtistInfo service="amazon" artistInfo={{ name: "Artist", images: "", followers: 0, external_urls: "" }} albumList={[album]} trackList={[]} searchQuery="" sortBy="default" currentPage={1} itemsPerPage={50} onSearchChange={noop} onSortChange={noop} onAlbumClick={noop} onArtistClick={noop} onPageChange={noop} activeTab="albums" onTabChange={noop}/></TooltipProvider>);
        const text = view.container.textContent!;
        expect(text).toContain("Artist");
        expect(text).not.toMatch(/follower|listener/i);
        // The albums/tracks summary has its own counts; any other "0" would be a leaked falsy value.
        expect(text.replace(/\d+ (albums?|tracks?)/gi, "")).not.toMatch(/(^|\D)0(\D|$)/);
        await view.unmount();
    });
});
