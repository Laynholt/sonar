// @vitest-environment jsdom
import { act } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { useDownloads } from "@/hooks/useDownloads";
import { guardContinue, guardWait } from "@/lib/downloads";
import { setLanguage } from "@/lib/i18n";
import { toastWithSound as toast } from "@/lib/toast-with-sound";
import { GuardDialog } from "./GuardDialog";
import { button, deferred, mount } from "../test/render";

vi.mock("@/hooks/useDownloads", () => ({ useDownloads: vi.fn() }));
vi.mock("@/lib/downloads", () => ({ guardWait: vi.fn(), guardContinue: vi.fn() }));
vi.mock("@/lib/toast-with-sound", () => ({ toastWithSound: { success: vi.fn(), error: vi.fn() } }));

type Site = { name: string; label: string; available: boolean; count: number; limit: number; waits: number[] | null; wait: number; asking: boolean; paused_until: number };
type Item = { status: string };
let view: Awaited<ReturnType<typeof mount>>;
const state = (list: Site[], items: Item[] = []) => ({ items, byTrackId: new Map(), activeCount: 0, sites: list, held: false }) as unknown as ReturnType<typeof useDownloads>;
const show = async (list: Site[], items: Item[] = []) => {
    vi.mocked(useDownloads).mockReturnValue(state(list, items));
    view = await mount(<GuardDialog/>);
};
const text = () => document.body.textContent;
const lucida: Site = { name: "lucida", label: "Lucida", available: true, count: 50, limit: 50, waits: [15, 30, 60, 120], wait: 30, asking: true, paused_until: 0 };
const double: Site = { name: "doubledouble", label: "DoubleDouble", available: true, count: 12, limit: 50, waits: [15, 30, 60, 120], wait: 30, asking: false, paused_until: 0 };

beforeEach(() => {
    vi.resetAllMocks();
    vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
});
afterEach(async () => { await view.unmount(); vi.unstubAllGlobals(); vi.restoreAllMocks(); setLanguage("en"); });

describe("GuardDialog", () => {
    it("titles the dialog with the site and its count", async () => {
        await show([lucida, double]);
        expect(text()).toContain("Lucida: 50 tracks this session");
        expect(text()).toContain("This is the limit from the plugin's settings.");
    });

    it("says what waiting does: the rest, the other site, the saved queue", async () => {
        await show([lucida, double], [
            ...Array(3).fill({ status: "completed" }), { status: "skipped" }, { status: "ignored" }, { status: "not_found" },
            { status: "matching" }, { status: "queued" }, { status: "downloading" }, { status: "review" }, { status: "failed" },
        ]);
        expect(text()).toContain("Lucida will rest for 30 min, then downloading resumes");
        expect(text()).toContain("DoubleDouble keeps downloading (12 of 50)");
        expect(text()).toContain("Queue saved: 6 done, 4 left");
    });

    it("shows the remaining pause of the other site", async () => {
        vi.spyOn(Date, "now").mockReturnValue(1_000_000);
        await show([lucida, { ...double, paused_until: 1_000_000 + 90_000 }]);
        expect(text()).toContain("DoubleDouble is paused for 1:30 more");
    });

    it("says when the other site is unavailable", async () => {
        await show([lucida, { ...double, available: false }]);
        expect(text()).toContain("DoubleDouble is unavailable");
    });

    it("leaves out the other site's line when there is none", async () => {
        await show([lucida]);
        expect(text()).not.toContain("keeps downloading");
    });

    it("offers each wait the site lists, the default first and primary, and passes the chosen one", async () => {
        vi.mocked(guardWait).mockResolvedValue(undefined);
        await show([{ ...lucida, waits: [10, 20, 40], wait: 20 }, double]);
        expect(text()).toContain("Lucida will rest for 20 min");
        // The default is first in the DOM (focused); flex-row-reverse puts it on the right.
        const waits = [...document.body.querySelectorAll("button")].map((b) => b.textContent).filter((x) => x?.startsWith("Wait"));
        expect(waits).toEqual(["Wait 20 min", "Wait 10 min", "Wait 40 min"]);
        expect(button(document.body, "Wait 20 min").className).toContain("bg-primary");
        expect(button(document.body, "Wait 10 min").className).not.toContain("bg-primary");
        await act(async () => button(document.body, "Wait 40 min").click());
        expect(guardWait).toHaveBeenCalledWith("lucida", 40);
    });

    it.each([null, []])("offers no wait when the site lists %j, only Continue", async (waits) => {
        await show([{ ...lucida, waits }, double]);
        expect([...document.body.querySelectorAll("button")].some((b) => b.textContent?.startsWith("Wait"))).toBe(false);
        expect(text()).not.toContain("will rest");
        expect(button(document.body, "Continue anyway")).toBeTruthy();
    });

    it("names a site by its label, else by its id", async () => {
        await show([{ ...lucida, label: "" }, { ...double, label: "Double Double" }]);
        expect(text()).toContain("lucida: 50 tracks this session");
        expect(text()).toContain("Double Double keeps downloading (12 of 50)");
    });

    it("reads in Russian as the mockup does", async () => {
        setLanguage("ru");
        await show([lucida, double], [{ status: "completed" }, { status: "queued" }]);
        expect(text()).toContain("Lucida: 50 треков за сессию");
        expect(text()).toContain("Lucida отдохнёт 30 мин, потом загрузка продолжится");
        expect(text()).toContain("DoubleDouble продолжает качать (12 из 50)");
        expect(text()).toContain("Очередь сохранена: 1 готово, 1 осталось");
        expect(button(document.body, "Подождать 30 мин")).toBeTruthy();
        expect(button(document.body, "Продолжить всё равно")).toBeTruthy();
    });

    it("waits or continues for the asking site", async () => {
        vi.mocked(guardWait).mockResolvedValue(undefined);
        vi.mocked(guardContinue).mockResolvedValue(undefined);
        await show([lucida, double]);
        await act(async () => button(document.body, "Wait 30 min").click());
        expect(guardWait).toHaveBeenCalledWith("lucida", 30);
        await act(async () => button(document.body, "Continue anyway").click());
        expect(guardContinue).toHaveBeenCalledWith("lucida");
    });

    it("disables both buttons while a call is in flight", async () => {
        const pending = deferred<void>();
        vi.mocked(guardWait).mockReturnValue(pending.promise);
        await show([lucida, double]);
        await act(async () => button(document.body, "Wait 30 min").click());
        expect(button(document.body, "Wait 30 min").disabled).toBe(true);
        expect(button(document.body, "Continue anyway").disabled).toBe(true);
        await act(async () => pending.resolve());
        expect(button(document.body, "Wait 30 min").disabled).toBe(false);
    });

    it("toasts a failed call", async () => {
        vi.mocked(guardWait).mockRejectedValue("nope");
        await show([lucida, double]);
        await act(async () => button(document.body, "Wait 30 min").click());
        expect(toast.error).toHaveBeenCalledWith("nope");
    });

    it("focuses Wait, not Continue, when it opens, and has no close button", async () => {
        await show([lucida, double]);
        expect(document.activeElement).toBe(button(document.body, "Wait 30 min"));
        expect(document.querySelector('[data-slot="dialog-close"]')).toBeNull();
    });

    it("does not close on Escape", async () => {
        await show([lucida, double]);
        await act(async () => { document.activeElement!.dispatchEvent(new KeyboardEvent("keydown", { key: "Escape", bubbles: true })); });
        expect(text()).toContain("Lucida: 50 tracks this session");
    });

    it("keeps its text while it closes", async () => {
        // jsdom runs no animations; reporting a closed-state animation makes Radix keep the node mounted, as in the app.
        const real = window.getComputedStyle.bind(window);
        vi.spyOn(window, "getComputedStyle").mockImplementation((el, pseudo) => new Proxy(real(el, pseudo), {
            get: (target, key) => {
                if (key === "animationName") return el.getAttribute("data-state") === "closed" ? "leave" : "none";
                const value = Reflect.get(target, key);
                return typeof value === "function" ? value.bind(target) : value;
            },
        }));
        await show([lucida, double]);
        vi.mocked(useDownloads).mockReturnValue(state([{ ...lucida, asking: false }, double]));
        await view.render(<GuardDialog/>);
        const dialog = document.querySelector('[role="dialog"]')!;
        expect(dialog.getAttribute("data-state")).toBe("closed");
        expect(dialog.textContent).toContain("Lucida: 50 tracks this session");
        expect(dialog.textContent).toContain("Lucida will rest for 30 min");
    });

    it("says the other site is also waiting when both are at their limit", async () => {
        await show([lucida, { ...double, asking: true, count: 50 }]);
        expect(text()).toContain("DoubleDouble is also waiting for an answer");
        expect(text()).not.toContain("keeps downloading");
    });

    it("counts the other site's pause down each second and stops the timer when done", async () => {
        vi.useFakeTimers({ now: 1_000_000 });
        try {
            await show([lucida, { ...double, paused_until: 1_000_000 + 90_000 }]);
            expect(text()).toContain("DoubleDouble is paused for 1:30 more");
            await act(async () => { vi.advanceTimersByTime(1000); });
            expect(text()).toContain("DoubleDouble is paused for 1:29 more");
            await view.unmount();
            // Radix leaves a zero-delay focus-restore timer; once it has run, a surviving interval would still be counted.
            await act(async () => { vi.advanceTimersByTime(5000); });
            expect(vi.getTimerCount()).toBe(0);
        } finally {
            vi.useRealTimers();
            view = await mount(<div/>);
        }
    });

    it("renders nothing when no site is asking", async () => {
        await show([{ ...lucida, asking: false }, double]);
        expect(text()).not.toContain("this session");
        expect(document.querySelector('[role="dialog"]')).toBeNull();
    });

    it("answers the first asking site first, then the next", async () => {
        vi.mocked(guardWait).mockResolvedValue(undefined);
        const both = [{ ...lucida, count: 7 }, { ...double, asking: true, count: 9 }];
        await show(both);
        expect(text()).toContain("Lucida: 7 tracks this session");
        await act(async () => button(document.body, "Wait 30 min").click());
        expect(guardWait).toHaveBeenCalledWith("lucida", 30);
        vi.mocked(useDownloads).mockReturnValue(state([{ ...both[0], asking: false }, both[1]]));
        await view.render(<GuardDialog/>);
        expect(text()).toContain("DoubleDouble: 9 tracks this session");
        await act(async () => button(document.body, "Wait 30 min").click());
        expect(guardWait).toHaveBeenLastCalledWith("doubledouble", 30);
    });
});
