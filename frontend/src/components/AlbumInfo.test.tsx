// @vitest-environment jsdom
import { describe, expect, it, vi } from "vitest";
import { button, mount } from "../test/render";
import { AlbumInfo } from "./AlbumInfo";

vi.mock("../../wailsjs/go/main/App", () => ({ GetPreviewURL: vi.fn() }));
vi.mock("../../wailsjs/runtime/runtime", () => ({ BrowserOpenURL: vi.fn() }));
vi.mock("@/lib/toast-with-sound", () => ({ toastWithSound: { error: vi.fn() } }));

const albumInfo = { name: "Album", artists: "Artist", images: "", release_date: "2020", total_tracks: 3 };
const noop = vi.fn();
const card = (props: Partial<Parameters<typeof AlbumInfo>[0]>) => <AlbumInfo service="amazon" sourceUrl="u" albumInfo={albumInfo} trackList={[]} searchQuery="" sortBy="default" currentPage={1} itemsPerPage={50} onSearchChange={noop} onSortChange={noop} onPageChange={noop} {...props}/>;

describe("AlbumInfo downloads", () => {
    it("starts the whole album from Download all", async () => {
        const onDownloadAll = vi.fn();
        const view = await mount(card({ canDownload: true, onDownloadAll }));

        button(view.container, "Download all").click();

        expect(onDownloadAll).toHaveBeenCalledTimes(1);
        await view.unmount();
    });

    it("disables Download all when the service cannot download", async () => {
        const view = await mount(card({ canDownload: false, onDownloadAll: vi.fn() }));

        expect(button(view.container, "Download all").disabled).toBe(true);
        await view.unmount();
    });

    it("shows progress and a Stop button while active", async () => {
        const onStopDownload = vi.fn();
        const view = await mount(card({ canDownload: true, onDownloadAll: vi.fn(), downloadProgress: { done: 1, total: 3, failed: 0, active: true }, onStopDownload }));

        expect(view.container.textContent).toContain("1 of 3 · 0 failed");
        button(view.container, "Stop").click();

        expect(onStopDownload).toHaveBeenCalledTimes(1);
        await view.unmount();
    });

    it("hides progress when idle", async () => {
        const view = await mount(card({ canDownload: true, onDownloadAll: vi.fn(), downloadProgress: { done: 3, total: 3, failed: 0, active: false } }));

        expect(view.container.textContent).not.toContain("3 of 3");
        expect(() => button(view.container, "Stop")).toThrow();
        await view.unmount();
    });
});
