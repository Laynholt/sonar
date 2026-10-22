// @vitest-environment jsdom
import { act } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { useDownloads } from "@/hooks/useDownloads";
import { cancelAllDownloads, cancelDownload, clearFinishedDownloads, refreshDownloads, resumeQueue, retryDownload, retryFailedDownloads, type DownloadItem } from "@/lib/downloads";
import { openPicker } from "@/lib/picker";
import { getSettings } from "@/lib/settings";
import { toastWithSound as toast } from "@/lib/toast-with-sound";
import { GetDownloadTags, GetSourceStatus, OpenFolder } from "../../wailsjs/go/main/App";
import { DownloadsPage } from "./DownloadsPage";
import { TooltipProvider } from "./ui/tooltip";
import { button, mount } from "../test/render";

vi.mock("../../wailsjs/go/main/App", () => ({ OpenFolder: vi.fn(), GetSourceStatus: vi.fn(), PassDoubleDoubleCheck: vi.fn(), GetDownloadTags: vi.fn() }));
vi.mock("@/hooks/useDownloads", () => ({ useDownloads: vi.fn() }));
vi.mock("@/lib/downloads", () => ({
    cancelDownload: vi.fn(), cancelAllDownloads: vi.fn(), retryDownload: vi.fn(), retryFailedDownloads: vi.fn(),
    clearFinishedDownloads: vi.fn(), resumeQueue: vi.fn(), refreshDownloads: vi.fn(),
}));
vi.mock("@/lib/picker", () => ({ openPicker: vi.fn() }));
vi.mock("@/lib/settings", async (original) => ({ ...await original<typeof import("@/lib/settings")>(), getSettings: vi.fn() }));
vi.mock("@/lib/toast-with-sound", () => ({ toastWithSound: { success: vi.fn(), error: vi.fn() } }));

const item = (id: string, status: string, extra: Partial<DownloadItem> = {}) =>
    ({ id, queue_id: "album:x", track: { title: `Song ${id}`, artist: "Artist" }, status, message: "", path: "", error: "", ...extra }) as DownloadItem;
const all = [
    item("q", "queued"),
    item("d", "downloading", { message: "lucida: Ripping track..." }),
    item("c", "completed", { path: "C:\\Music\\c.flac" }),
    item("s", "skipped", { path: "C:\\Music\\s.flac" }),
    item("f", "failed", { error: "boom" }),
    item("x", "cancelled"),
];
const noSites: Record<string, unknown>[] = [];
let view: Awaited<ReturnType<typeof mount>>;
const show = async (items: DownloadItem[], sites: Record<string, unknown>[] = noSites, activeCount = 2, held = false) => {
    vi.mocked(useDownloads).mockReturnValue({ items, byTrackId: new Map(), activeCount, sites, held } as unknown as ReturnType<typeof useDownloads>);
    view = await mount(<TooltipProvider><DownloadsPage/></TooltipProvider>);
};
const titles = () => [...view.container.querySelectorAll("tbody tr")].map((r) => r.querySelector("td:nth-child(2) div")!.textContent);
const rows = () => view.container.querySelectorAll("tbody tr").length;
const row = (id: string) => [...view.container.querySelectorAll("tbody tr")].find((r) => r.textContent?.includes(`Song ${id}`))!;
const press = (el: Element, label: string) => act(async () => el.querySelector<HTMLElement>(`[aria-label="${label}"]`)!.click());

beforeEach(() => {
    vi.resetAllMocks();
    vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
    vi.mocked(getSettings).mockReturnValue({ downloadSource: "auto" } as ReturnType<typeof getSettings>);
    vi.mocked(OpenFolder).mockResolvedValue(undefined);
    vi.mocked(GetSourceStatus).mockResolvedValue([{ name: "doubledouble", available: false, needs_check: true, reason: "", count: 0, limit: 0, asking: false, paused_until: 0 }]);
    for (const action of [cancelDownload, cancelAllDownloads, retryDownload, retryFailedDownloads, clearFinishedDownloads, resumeQueue]) vi.mocked(action).mockResolvedValue(undefined);
});
afterEach(async () => { await view.unmount(); vi.unstubAllGlobals(); });

describe("DownloadsPage", () => {
    it("refreshes the queue when it opens", async () => {
        await show(all);
        expect(refreshDownloads).toHaveBeenCalled();
    });

    it("says nothing here for an empty tab while other tabs have items", async () => {
        await show([item("q", "queued")]);
        await act(async () => button(view.container, "Failed").click());
        expect(view.container.textContent).toContain("Nothing here");
        expect(view.container.textContent).not.toContain("No downloads yet");
    });

    it("filters rows by tab", async () => {
        await show(all);
        const titles = () => [...view.container.querySelectorAll("tbody tr")].map((r) => r.querySelector("td:nth-child(2) div")!.textContent);
        expect(titles()).toEqual(["Song q", "Song d", "Song c", "Song s", "Song f", "Song x"]);
        await act(async () => button(view.container, "Active").click());
        expect(titles()).toEqual(["Song q", "Song d"]);
        await act(async () => button(view.container, "Done").click());
        expect(titles()).toEqual(["Song c", "Song s"]);
        await act(async () => button(view.container, "Failed").click());
        expect(titles()).toEqual(["Song f", "Song x"]);
    });

    it("shows the translated status and the live message", async () => {
        await show(all);
        for (const label of ["Queued", "Downloading", "Completed", "Skipped", "Failed", "Cancelled"]) expect(view.container.textContent).toContain(label);
        expect(view.container.textContent).toContain("lucida: Ripping track...");
        expect(view.container.querySelector('[title="boom"]')).not.toBeNull();
    });

    it("retries a failed row, cancels a downloading row, opens the folder of a completed one", async () => {
        await show(all);
        await press(row("f"), "Retry");
        expect(retryDownload).toHaveBeenCalledWith("f");
        await press(row("d"), "Stop");
        expect(cancelDownload).toHaveBeenCalledWith("d");
        await press(row("c"), "Show in folder");
        expect(OpenFolder).toHaveBeenCalledWith("C:\\Music\\c.flac");
    });

    it("reports a failed row action as a toast", async () => {
        vi.mocked(retryDownload).mockRejectedValue("nope");
        await show(all);
        await press(row("f"), "Retry");
        expect(toast.error).toHaveBeenCalledWith("nope");
    });

    it("runs the header actions", async () => {
        await show(all);
        await act(async () => button(view.container, "Retry failed").click());
        await act(async () => button(view.container, "Stop all").click());
        await act(async () => button(view.container, "Clear finished").click());
        expect(retryFailedDownloads).toHaveBeenCalled();
        expect(cancelAllDownloads).toHaveBeenCalled();
        expect(clearFinishedDownloads).toHaveBeenCalled();
    });

    it("disables the header actions that have nothing to act on", async () => {
        await show([item("c", "completed", { path: "p" })], noSites, 0);
        expect(button(view.container, "Retry failed").disabled).toBe(true);
        expect(button(view.container, "Stop all").disabled).toBe(true);
        expect(button(view.container, "Clear finished").disabled).toBe(false);
    });

    it("enables Retry failed only for failed items, not cancelled ones", async () => {
        await show([item("x", "cancelled"), item("c", "completed", { path: "p" })], noSites, 0);
        expect(button(view.container, "Retry failed").disabled).toBe(true);
        vi.mocked(useDownloads).mockReturnValue({ items: [item("f", "failed")], byTrackId: new Map(), activeCount: 0, sites: noSites, held: false } as unknown as ReturnType<typeof useDownloads>);
        await view.render(<TooltipProvider><DownloadsPage/></TooltipProvider>);
        expect(button(view.container, "Retry failed").disabled).toBe(false);
    });

    it("offers the check only in doubledouble mode", async () => {
        vi.mocked(getSettings).mockReturnValue({ downloadSource: "doubledouble" } as ReturnType<typeof getSettings>);
        await show(all);
        expect(button(view.container, "Pass check")).toBeTruthy();
        await view.unmount();
        vi.mocked(getSettings).mockReturnValue({ downloadSource: "auto" } as ReturnType<typeof getSettings>);
        await show(all);
        expect([...view.container.querySelectorAll("button")].some((b) => b.textContent === "Pass check")).toBe(false);
    });

    it("shows the empty state", async () => {
        await show([], noSites, 0);
        expect(view.container.textContent).toContain("No downloads yet");
        expect(rows()).toBe(0);
    });

    it("has a tab for the tracks that wait for a decision, with their count", async () => {
        await show([item("r1", "review"), item("r2", "review"), item("q", "queued"), item("m", "matching")]);
        const tab = button(view.container, "Awaiting decision 2");
        await act(async () => tab.click());
        expect(titles()).toEqual(["Song r1", "Song r2"]);
    });

    it("shows no count on the tab while nothing waits", async () => {
        await show([item("q", "queued")]);
        expect(button(view.container, "Awaiting decision")).toBeTruthy();
    });

    it("lists matching under Active and not found and ignored under Done", async () => {
        await show([item("m", "matching"), item("n", "not_found"), item("i", "ignored"), item("c", "completed")]);
        await act(async () => button(view.container, "Active").click());
        expect(titles()).toEqual(["Song m"]);
        await act(async () => button(view.container, "Done").click());
        expect(titles()).toEqual(["Song n", "Song i", "Song c"]);
    });

    it("labels the matching, review, not found and ignored rows", async () => {
        await show([item("m", "matching"), item("r", "review"), item("n", "not_found"), item("i", "ignored")]);
        expect(row("m").textContent).toContain("Searching Amazon…");
        expect(row("r").textContent).toContain("Needs a decision");
        expect(row("n").textContent).toContain("Not found");
        expect(row("i").textContent).toContain("Skipped manually");
    });

    it("opens the picker for a review row", async () => {
        await show([item("a", "review"), item("r", "review")]);
        await act(async () => button(row("r"), "Choose…").click());
        expect(openPicker).toHaveBeenCalledWith("r");
        expect(openPicker).toHaveBeenCalledTimes(1);
    });

    it("counts not found and ignored rows as finished", async () => {
        await show([item("q", "queued"), item("n", "not_found"), item("i", "ignored"), item("c", "completed")]);
        expect(view.container.textContent).toContain("Downloading: 3 of 4");
    });

    it("shows a bar with Continue while the queue is held", async () => {
        await show([item("q", "queued")], noSites, 0, true);
        await act(async () => button(view.container, "Continue").click());
        expect(resumeQueue).toHaveBeenCalled();
    });

    it("shows no hold bar when the queue runs", async () => {
        await show(all);
        expect([...view.container.querySelectorAll("button")].some((b) => b.textContent === "Continue")).toBe(false);
    });

    it("reports a failed resume as a toast", async () => {
        vi.mocked(resumeQueue).mockRejectedValue("nope");
        await show([item("q", "queued")], noSites, 0, true);
        await act(async () => button(view.container, "Continue").click());
        expect(toast.error).toHaveBeenCalledWith("nope");
    });

    it("shows a paused site's countdown here too, since the widget is hidden on this page", async () => {
        const now = vi.spyOn(Date, "now").mockReturnValue(1_000_000);
        await show(all, [{ name: "lucida", available: true, count: 50, limit: 50, asking: false, paused_until: 1_000_000 + 24 * 60_000 + 13_000 }]);
        expect(view.container.textContent).toContain("Lucida");
        expect(view.container.textContent).toContain("paused · 24:13");
        now.mockRestore();
    });

    it("sums up the queue and shows each download's size", async () => {
        const MB = 1024 * 1024;
        await show([...all.slice(0, 1), item("d", "downloading", { bytes: 2 * MB, size: 4 * MB }), item("c", "completed", { path: "C:\Music\c.flac", bytes: 30 * MB, size: 30 * MB })]);
        expect(view.container.textContent).toContain("Downloading: 1 of 3");
        expect(view.container.textContent).toContain("50%");
        expect(row("d").textContent).toContain("2.0 of 4.0 MB");
        expect(row("d").querySelector("[role=progressbar]")).not.toBeNull();
        expect(row("c").textContent).toContain("30.0 of 30.0 MB");
    });

    it("shows the tags a finished file carries", async () => {
        vi.mocked(GetDownloadTags).mockResolvedValue({ path: "C:\Music\c.flac", format: "FLAC", size: 1024 * 1024, fields: [{ key: "title", value: "Song c" }, { key: "track", value: "3/12" }], extra: [{ key: "ISRC", value: "USUG1" }], cover: "" } as never);
        await show(all);
        expect(row("f").querySelector('[aria-label="Tags in the file"]')).toBeNull();
        await press(row("c"), "Tags in the file");
        expect(GetDownloadTags).toHaveBeenCalledWith("c");
        const dialog = document.querySelector("[role=dialog]")!;
        for (const text of ["Title", "Song c", "Track", "3/12", "ISRC", "USUG1", "FLAC", "No cover in the file"]) expect(dialog.textContent).toContain(text);
    });
});
