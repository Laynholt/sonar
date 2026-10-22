// @vitest-environment jsdom
import { act } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { clearQueue, resumeQueue } from "@/lib/downloads";
import { setLanguage } from "@/lib/i18n";
import { toastWithSound as toast } from "@/lib/toast-with-sound";
import { GetPendingQueue } from "../../wailsjs/go/main/App";
import { ResumeDialog } from "./ResumeDialog";
import { button, deferred, mount } from "../test/render";

vi.mock("../../wailsjs/go/main/App", () => ({ GetPendingQueue: vi.fn() }));
vi.mock("@/lib/downloads", () => ({ resumeQueue: vi.fn(), clearQueue: vi.fn() }));
vi.mock("@/lib/toast-with-sound", () => ({ toastWithSound: { success: vi.fn(), error: vi.fn() } }));

let view: Awaited<ReturnType<typeof mount>>;
const pending = (tracks: number, review = 0, collections: string[] = []) => vi.mocked(GetPendingQueue).mockResolvedValue({ tracks, review, collections } as Awaited<ReturnType<typeof GetPendingQueue>>);
const show = async () => { view = await mount(<ResumeDialog/>); };
const text = () => document.body.textContent;
const closed = () => document.querySelector('[role="dialog"]') === null;

beforeEach(() => {
    vi.resetAllMocks();
    vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
});
afterEach(async () => { await view.unmount(); vi.unstubAllGlobals(); setLanguage("en"); });

describe("ResumeDialog", () => {
    it("asks once on mount and shows nothing when no track is held", async () => {
        pending(0, 0, []);
        await show();
        expect(GetPendingQueue).toHaveBeenCalledTimes(1);
        expect(closed()).toBe(true);
        await view.render(<ResumeDialog/>);
        expect(GetPendingQueue).toHaveBeenCalledTimes(1);
    });

    it("stays quiet when the question fails", async () => {
        vi.mocked(GetPendingQueue).mockRejectedValue(new Error("down"));
        await show();
        expect(closed()).toBe(true);
    });

    it("names the track count and the collections", async () => {
        pending(112, 0, ["Discover Weekly", "Album A"]);
        await show();
        expect(text()).toContain("Unfinished download");
        expect(text()).toContain("Left unfinished last time: 112 tracks from «Discover Weekly», «Album A».");
        expect(text()).not.toContain("awaiting your decision");
    });

    it("shows three collections and counts the rest", async () => {
        pending(30, 0, ["A", "B", "C", "D", "E"]);
        await show();
        expect(text()).toContain("from «A», «B», «C» and 2 more.");
        expect(text()).not.toContain("«D»");
    });

    it("leaves out the from-part when it has no collection names", async () => {
        pending(1, 0, []);
        await show();
        expect(text()).toContain("Left unfinished last time: 1 track.");
    });

    it("adds the review count with its plural when it is above 0", async () => {
        pending(5, 1, ["X"]);
        await show();
        expect(text()).toContain("1 track is awaiting your decision.");
        await view.unmount();
        pending(5, 3, ["X"]);
        await show();
        expect(text()).toContain("3 tracks are awaiting your decision.");
    });

    it("reads in Russian as the mockup does", async () => {
        setLanguage("ru");
        pending(112, 3, ["Discover Weekly", "Альбом", "Ещё один", "Четвёртый"]);
        await show();
        expect(text()).toContain("Незаконченная загрузка");
        expect(text()).toContain("В прошлый раз не докачали: 112 треков из «Discover Weekly», «Альбом», «Ещё один» и ещё 1.");
        expect(text()).toContain("3 трека ждут твоего решения.");
        for (const label of ["Очистить очередь", "Позже", "Продолжить"]) expect(button(document.body, label)).toBeTruthy();
    });

    it("continues the queue and closes", async () => {
        pending(2);
        vi.mocked(resumeQueue).mockResolvedValue(undefined);
        await show();
        expect(document.activeElement).toBe(button(document.body, "Continue"));
        await act(async () => button(document.body, "Continue").click());
        expect(resumeQueue).toHaveBeenCalledTimes(1);
        expect(clearQueue).not.toHaveBeenCalled();
        expect(text()).not.toContain("Unfinished download");
    });

    it("clears the queue and closes", async () => {
        pending(2);
        vi.mocked(clearQueue).mockResolvedValue(undefined);
        await show();
        await act(async () => button(document.body, "Clear queue").click());
        expect(clearQueue).toHaveBeenCalledTimes(1);
        expect(text()).not.toContain("Unfinished download");
    });

    it("closes on Later without touching the queue", async () => {
        pending(2);
        await show();
        await act(async () => button(document.body, "Later").click());
        expect(resumeQueue).not.toHaveBeenCalled();
        expect(clearQueue).not.toHaveBeenCalled();
        expect(text()).not.toContain("Unfinished download");
    });

    it("disables the buttons while a call is in flight", async () => {
        pending(2);
        const call = deferred<void>();
        vi.mocked(resumeQueue).mockReturnValue(call.promise);
        await show();
        await act(async () => button(document.body, "Continue").click());
        expect(button(document.body, "Later").disabled).toBe(true);
        await act(async () => call.resolve());
    });

    it("keeps the dialog and toasts when the call fails", async () => {
        pending(2);
        vi.mocked(clearQueue).mockRejectedValue("nope");
        await show();
        await act(async () => button(document.body, "Clear queue").click());
        expect(toast.error).toHaveBeenCalledWith("nope");
        expect(text()).toContain("Unfinished download");
    });
});
