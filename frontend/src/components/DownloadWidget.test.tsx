// @vitest-environment jsdom
import { act } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { useDownloads } from "@/hooks/useDownloads";
import type { DownloadItem } from "@/lib/downloads";
import { openPicker } from "@/lib/picker";
import { DownloadWidget } from "./DownloadWidget";
import { button, mount } from "../test/render";

vi.mock("@/hooks/useDownloads", () => ({ useDownloads: vi.fn() }));
vi.mock("@/lib/picker", () => ({ openPicker: vi.fn() }));

const MB = 1024 * 1024;
const item = (id: string, status: string, extra: Partial<DownloadItem> = {}) =>
    ({ id, queue_id: "album:x", track: { title: `Song ${id}`, artist: "Artist" }, status, message: "", path: "", error: "", bytes: 0, size: 0, ...extra }) as DownloadItem;
const active = (items: DownloadItem[]) => items.filter((i) => ["matching", "queued", "downloading"].includes(i.status)).length;
type Site = { name: string; available: boolean; count: number; limit: number; asking: boolean; paused_until: number };
const site = (name: string, extra: Partial<Site> = {}): Site => ({ name, available: true, count: 0, limit: 0, asking: false, paused_until: 0, ...extra });
const set = (items: DownloadItem[], sites: Site[] = []) =>
    vi.mocked(useDownloads).mockReturnValue({ items, byTrackId: new Map(), activeCount: active(items), sites, held: false } as unknown as ReturnType<typeof useDownloads>);

let view: Awaited<ReturnType<typeof mount>>;
const onOpen = vi.fn();
const widget = (hidden = false) => <DownloadWidget hidden={hidden} onOpen={onOpen}/>;
const show = async (items: DownloadItem[], hidden = false, sites: Site[] = []) => { set(items, sites); await view.render(widget(hidden)); };
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

    it("shows the speed from the bytes between polls", async () => {
        const now = vi.spyOn(Date, "now").mockReturnValue(10_000);
        await show([item("a", "downloading", { bytes: 0, size: 4 * MB })]);
        now.mockReturnValue(11_000);
        await show([item("a", "downloading", { bytes: 2 * MB, size: 4 * MB })]);
        expect(text()).toContain("1.0 MB/s");
        now.mockRestore();
    });

    it("snaps to the corner it is dropped nearest and remembers it", async () => {
        await show([item("a", "downloading")]);
        const card = view.container.querySelector<HTMLElement>("[role=status]")!;
        card.getBoundingClientRect = () => ({ left: 900, top: 40, width: 320, height: 120 }) as DOMRect;
        const fire = (type: string, x: number, y: number) => act(async () => card.dispatchEvent(new MouseEvent(type, { bubbles: true, button: 0, clientX: x, clientY: y })));
        await fire("pointerdown", 500, 600);
        await fire("pointermove", 800, 100);
        expect(card.style.transform).toBe("translate(300px, -500px)");
        await fire("pointerup", 800, 100);
        expect(card.className).toContain("top-14 right-4");
        expect(localStorage.getItem("sonar.downloadWidgetCorner")).toBe("top-right");
        localStorage.clear();
    });

    it("stays closed until the next run", async () => {
        await show([item("a", "downloading")]);
        await act(async () => view.container.querySelector<HTMLElement>('[aria-label="Hide"]')!.click());
        expect(view.container.querySelector("[role=status]")).toBeNull();
        await show([item("a", "completed")]);
        await show([item("a", "completed"), item("b", "queued")]);
        expect(text()).toContain("0 of 1");
    });

    it("counts not found and ignored tracks as finished", async () => {
        await show([item("a", "completed"), item("b", "not_found"), item("c", "ignored"), item("d", "queued")]);
        expect(text()).toContain("Downloading: 3 of 4");
        await show([item("a", "completed"), item("b", "not_found"), item("c", "ignored"), item("d", "completed")]);
        expect(text()).toContain("Finished: 4 of 4");
    });

    it("says how many tracks wait for a decision and opens the picker", async () => {
        await show([item("a", "downloading"), item("b", "review"), item("c", "review")]);
        expect(text()).toContain("⚠ 2 tracks awaiting your decision");
        await act(async () => button(view.container, "Resolve").click());
        expect(openPicker).toHaveBeenCalledWith();
    });

    it("has no decision line without review items", async () => {
        await show([item("a", "downloading")]);
        expect(text()).not.toContain("awaiting your decision");
    });

    describe("site rows", () => {
        const now = 1_000_000;
        beforeEach(() => { vi.spyOn(Date, "now").mockReturnValue(now); });
        afterEach(() => { vi.mocked(Date.now).mockRestore(); });

        it("shows a downloading site with its count and limit", async () => {
            await show([item("a", "downloading", { site: "lucida" }), item("b", "queued")], false, [site("lucida", { count: 12, limit: 50 }), site("doubledouble")]);
            expect(text()).toContain("lucida");
            expect(text()).toContain("downloading · 12/50");
        });

        it("leaves the limit out when it is 0", async () => {
            await show([item("a", "downloading", { site: "lucida" })], false, [site("lucida", { count: 3 })]);
            expect(text()).toContain("downloading · 3");
            expect(text()).not.toContain("3/");
        });

        it("counts a pause down to its end", async () => {
            await show([item("a", "downloading", { site: "doubledouble" })], false, [site("lucida", { paused_until: now + (24 * 60 + 13) * 1000 }), site("doubledouble", { count: 12, limit: 50 })]);
            expect(text()).toContain("paused · 24:13");
            expect(text()).toContain("downloading · 12/50");
            vi.mocked(Date.now).mockReturnValue(now + 13_000);
            await show([item("a", "downloading", { site: "doubledouble" })], false, [site("lucida", { paused_until: now + (24 * 60 + 13) * 1000 }), site("doubledouble", { count: 12, limit: 50 })]);
            expect(text()).toContain("paused · 24:00");
        });

        it("ticks by itself", async () => {
            vi.useFakeTimers({ toFake: ["setInterval", "clearInterval"] });
            await show([item("a", "queued")], false, [site("lucida", { paused_until: now + 90_000 })]);
            expect(text()).toContain("paused · 1:30");
            vi.mocked(Date.now).mockReturnValue(now + 2_000);
            await act(async () => { vi.advanceTimersByTime(1000); });
            expect(text()).toContain("paused · 1:28");
            vi.useRealTimers();
        });

        it("shows a site that waits for the user's answer", async () => {
            await show([item("a", "queued")], false, [site("lucida", { asking: true })]);
            expect(text()).toContain("waiting for your answer");
        });

        it("shows an unavailable site", async () => {
            await show([item("a", "queued")], false, [site("lucida", { available: false })]);
            expect(text()).toContain("unavailable");
        });

        it("shows nothing for sites that have nothing to report", async () => {
            await show([item("a", "downloading", { site: "doubledouble" })], false, [site("lucida"), site("doubledouble", { count: 1 })]);
            expect(text()).not.toContain("lucida");
        });

        it("ignores a pause that already ended", async () => {
            await show([item("a", "queued")], false, [site("lucida", { paused_until: now - 1000 })]);
            expect(text()).not.toContain("paused");
        });
    });
});
