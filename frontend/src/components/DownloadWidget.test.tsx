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

    describe("badge", () => {
        const hide = () => act(async () => view.container.querySelector<HTMLElement>('[aria-label="Hide"]')!.click());
        const badge = () => view.container.querySelector<HTMLElement>("button[aria-label^='Downloading']");
        const running = () => [item("a", "downloading", { bytes: 62, size: 100 })];
        const fire = (type: string, x: number) => act(async () => badge()!.dispatchEvent(new MouseEvent(type, { bubbles: true, button: 0, clientX: x, clientY: 0 })));

        it("collapses on X while running and expands on click", async () => {
            await show(running());
            await hide();
            expect(view.container.querySelector("[role=status]")).toBeNull();
            expect(badge()!.getAttribute("aria-label")).toMatch(/62%.*MB\/s/);
            await act(async () => badge()!.click());
            expect(view.container.querySelector("[role=status]")).not.toBeNull();
            expect(badge()).toBeNull();
        });

        it("shows a check when finished", async () => {
            await show(running());
            await hide();
            await show([item("a", "completed")]);
            expect(view.container.querySelector("[data-testid=badge-done]")).not.toBeNull();
            expect(text()).not.toContain("MB/s");
        });

        it("does not expand after a drag", async () => {
            await show(running());
            await hide();
            await fire("pointerdown", 100);
            await fire("pointermove", 130);
            await fire("pointerup", 130);
            expect(badge()).not.toBeNull();
            expect(view.container.querySelector("[role=status]")).toBeNull();
        });

        it("expands on a press without movement", async () => {
            await show(running());
            await hide();
            await fire("pointerdown", 100);
            await fire("pointerup", 101);
            expect(view.container.querySelector("[role=status]")).not.toBeNull();
        });

        it("closing a finished run hides everything", async () => {
            await show([item("a", "completed")]);
            await show([item("a", "downloading")]);
            await show([item("a", "completed")]);
            await hide();
            expect(view.container.querySelector("[role=status]")).toBeNull();
            expect(view.container.querySelector("button[aria-label]")).toBeNull();
        });

        it("has an amber dot for a review item or a site that asks", async () => {
            await show([...running(), item("r", "review")]);
            await hide();
            expect(view.container.querySelector("[data-testid=badge-attention]")).not.toBeNull();
            await show(running(), false, [site("lucida", { asking: true })]);
            expect(view.container.querySelector("[data-testid=badge-attention]")).not.toBeNull();
            await show(running());
            expect(view.container.querySelector("[data-testid=badge-attention]")).toBeNull();
        });

        it("starts a new run from idle expanded, counting only its own items", async () => {
            await show(running());
            await hide();
            await show([item("a", "completed")]);
            await show([item("a", "completed"), item("b", "downloading")]);
            expect(view.container.querySelector("[role=status]")).not.toBeNull();
            expect(text()).toContain("0 of 1");
        });

        it("labels a finished badge as finished", async () => {
            await show(running());
            await hide();
            await show([item("a", "completed")]);
            expect(view.container.querySelector("button[aria-label=\"Downloads finished\"]")).not.toBeNull();
        });

        it("starts the next run expanded", async () => {
            await show(running());
            await hide();
            await show([item("a", "completed")]);
            await show([]);
            await show([item("b", "downloading")]);
            expect(view.container.querySelector("[role=status]")).not.toBeNull();
        });
    });

    it("stays closed until the next run", async () => {
        await show([item("a", "queued")]);
        await show([item("a", "completed")]);
        await act(async () => view.container.querySelector<HTMLElement>('[aria-label="Hide"]')!.click());
        expect(view.container.querySelector("[role=status]")).toBeNull();
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

    it("counts every review item, also those from before this run", async () => {
        await show([item("rv", "review")]);
        await show([item("rv", "review"), item("a", "downloading")]);
        expect(text()).toContain("⚠ 1 track awaiting your decision");
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
            expect(text()).toContain("Lucida");
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

        it("lists an idle site as ready, under its display name", async () => {
            await show([item("a", "downloading", { site: "doubledouble" })], false, [site("lucida"), site("doubledouble", { count: 1 })]);
            expect(text()).toContain("Lucida");
            expect(text()).toContain("ready");
            expect(text()).toContain("DoubleDouble");
            expect(text()).not.toContain("lucida");
        });

        it("keeps an unknown site's own name", async () => {
            await show([item("a", "queued")], false, [site("newsite")]);
            expect(text()).toContain("newsite");
        });

        it("ignores a pause that already ended", async () => {
            await show([item("a", "queued")], false, [site("lucida", { paused_until: now - 1000 })]);
            expect(text()).not.toContain("paused");
            expect(text()).toContain("ready");
        });
    });
});
