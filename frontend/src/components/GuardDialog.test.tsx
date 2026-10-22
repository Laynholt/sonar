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
const show = async (guard: { site: string; count: number; asking: boolean; paused_until: number }) => {
    vi.mocked(useDownloads).mockReturnValue({ items: [], byTrackId: new Map(), activeCount: 0, guard } as unknown as ReturnType<typeof useDownloads>);
    view = await mount(<GuardDialog/>);
};
const asking = { site: "lucida", count: 50, asking: true, paused_until: 0 };

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
        expect(guardWait).toHaveBeenCalled();
        await act(async () => button(document.body, "I don't care, continue").click());
        expect(guardContinue).toHaveBeenCalled();
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

    it("cannot be dismissed by Escape and has no close button", async () => {
        await show(asking);
        await act(async () => { document.activeElement?.dispatchEvent(new KeyboardEvent("keydown", { key: "Escape", bubbles: true })); });
        expect(document.body.textContent).toContain("Slow down a little");
        expect(document.querySelector('[data-slot="dialog-close"]')).toBeNull();
    });

    it("renders nothing when not asking", async () => {
        await show({ ...asking, asking: false });
        expect(document.body.textContent).not.toContain("Slow down a little");
    });
});
