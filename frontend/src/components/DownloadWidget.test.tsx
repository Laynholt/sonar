// @vitest-environment jsdom
import { act } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { useDownloads } from "@/hooks/useDownloads";
import type { DownloadItem } from "@/lib/downloads";
import { DownloadWidget } from "./DownloadWidget";
import { button, mount } from "../test/render";

vi.mock("@/hooks/useDownloads", () => ({ useDownloads: vi.fn() }));

const MB = 1024 * 1024;
const item = (id: string, status: string, extra: Partial<DownloadItem> = {}) =>
    ({ id, queue_id: "album:x", track: { title: `Song ${id}`, artist: "Artist" }, status, message: "", path: "", error: "", bytes: 0, size: 0, ...extra }) as DownloadItem;
const active = (items: DownloadItem[]) => items.filter((i) => i.status === "queued" || i.status === "downloading").length;
const set = (items: DownloadItem[]) =>
    vi.mocked(useDownloads).mockReturnValue({ items, byTrackId: new Map(), activeCount: active(items), guard: {} } as unknown as ReturnType<typeof useDownloads>);

let view: Awaited<ReturnType<typeof mount>>;
const onOpen = vi.fn();
const widget = (hidden = false) => <DownloadWidget hidden={hidden} onOpen={onOpen}/>;
const show = async (items: DownloadItem[], hidden = false) => { set(items); await view.render(widget(hidden)); };
const text = () => view.container.textContent ?? "";

beforeEach(async () => {
    vi.resetAllMocks();
    vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
    set([item("old", "completed")]);
    view = await mount(widget());
});
afterEach(async () => { await view.unmount(); vi.unstubAllGlobals(); });

describe("DownloadWidget", () => {
    it("stays away while nothing is downloading", () => {
        expect(view.container.querySelector("[role=status]")).toBeNull();
    });

    it("counts only the current run and shows sizes", async () => {
        await show([item("old", "completed"), item("a", "downloading", { bytes: 2 * MB, size: 4 * MB }), item("b", "queued")]);
        expect(text()).toContain("0 of 2");
        expect(text()).toContain("Song a");
        expect(text()).toContain("2.0 of 4.0 MB");
        expect(text()).toContain("In queue: 1");
        await show([item("old", "completed"), item("a", "completed", { bytes: 4 * MB, size: 4 * MB }), item("b", "failed")]);
        expect(text()).toContain("Finished: 1 of 2");
        expect(text()).toContain("Failed: 1");
    });

    it("hides on the downloads page and opens it from the link", async () => {
        await show([item("a", "downloading")], true);
        expect(view.container.querySelector("[role=status]")).toBeNull();
        await show([item("a", "downloading")]);
        await act(async () => button(view.container, "Open downloads").click());
        expect(onOpen).toHaveBeenCalled();
    });

    it("stays closed until the next run", async () => {
        await show([item("a", "downloading")]);
        await act(async () => view.container.querySelector<HTMLElement>('[aria-label="Hide"]')!.click());
        expect(view.container.querySelector("[role=status]")).toBeNull();
        await show([item("a", "completed")]);
        await show([item("a", "completed"), item("b", "queued")]);
        expect(text()).toContain("0 of 1");
    });
});
