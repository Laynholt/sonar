// @vitest-environment jsdom
import { act } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { useDownloads } from "@/hooks/useDownloads";
import { guardContinue, guardWait } from "@/lib/downloads";
import { toastWithSound as toast } from "@/lib/toast-with-sound";
import { GuardDialog } from "./GuardDialog";
import { button, deferred, mount } from "../test/render";

vi.mock("@/hooks/useDownloads", () => ({ useDownloads: vi.fn() }));
vi.mock("@/lib/downloads", () => ({ guardWait: vi.fn(), guardContinue: vi.fn() }));
vi.mock("@/lib/toast-with-sound", () => ({ toastWithSound: { success: vi.fn(), error: vi.fn() } }));

let view: Awaited<ReturnType<typeof mount>>;
const sites = (...list: Site[]) => ({ items: [], byTrackId: new Map(), activeCount: 0, sites: list, held: false }) as unknown as ReturnType<typeof useDownloads>;
const show = async (...list: Site[]) => {
    vi.mocked(useDownloads).mockReturnValue(sites(...list));
    view = await mount(<GuardDialog/>);
};
type Site = { name: string; count: number; limit: number; asking: boolean; paused_until: number };
const asking: Site = { name: "lucida", count: 50, limit: 50, asking: true, paused_until: 0 };

beforeEach(() => {
    vi.resetAllMocks();
    vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
});
afterEach(async () => { await view.unmount(); vi.unstubAllGlobals(); });

describe("GuardDialog", () => {
    it("names the site and the count", async () => {
        await show(asking);
        expect(document.body.textContent).toContain("50 tracks in a row came from lucida");
    });

    it("waits or continues", async () => {
        vi.mocked(guardWait).mockResolvedValue(undefined);
        vi.mocked(guardContinue).mockResolvedValue(undefined);
        await show(asking);
        await act(async () => button(document.body, "OK, I'll wait").click());
        expect(guardWait).toHaveBeenCalledWith("lucida");
        await act(async () => button(document.body, "I don't care, continue").click());
        expect(guardContinue).toHaveBeenCalledWith("lucida");
    });

    it("disables both buttons while a call is in flight", async () => {
        const pending = deferred<void>();
        vi.mocked(guardWait).mockReturnValue(pending.promise);
        await show(asking);
        await act(async () => button(document.body, "OK, I'll wait").click());
        expect(button(document.body, "OK, I'll wait").disabled).toBe(true);
        expect(button(document.body, "I don't care, continue").disabled).toBe(true);
        await act(async () => pending.resolve());
        expect(button(document.body, "OK, I'll wait").disabled).toBe(false);
    });

    it("toasts a failed call", async () => {
        vi.mocked(guardWait).mockRejectedValue("nope");
        await show(asking);
        await act(async () => button(document.body, "OK, I'll wait").click());
        expect(toast.error).toHaveBeenCalledWith("nope");
    });

    it("focuses Wait, not Continue, when it opens", async () => {
        await show(asking);
        expect(document.activeElement).toBe(button(document.body, "OK, I'll wait"));
    });

    it("has no close button and goes away once the guard stops asking", async () => {
        await show(asking);
        expect(document.querySelector('[data-slot="dialog-close"]')).toBeNull();
        vi.mocked(useDownloads).mockReturnValue(sites({ ...asking, asking: false }));
        await view.render(<GuardDialog/>);
        expect(document.body.textContent).not.toContain("Slow down a little");
    });

    it("asks for the first site that is asking and answers for that site", async () => {
        vi.mocked(guardWait).mockResolvedValue(undefined);
        await show({ ...asking, name: "doubledouble", asking: false }, { ...asking, name: "amazon", count: 7 }, { ...asking, name: "other" });
        expect(document.body.textContent).toContain("7 tracks in a row came from amazon");
        await act(async () => button(document.body, "OK, I'll wait").click());
        expect(guardWait).toHaveBeenCalledWith("amazon");
    });

    it("renders nothing when not asking", async () => {
        await show({ ...asking, asking: false });
        expect(document.body.textContent).not.toContain("Slow down a little");
    });
});
