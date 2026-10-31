// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { useDownloads } from "@/hooks/useDownloads";
import type { DownloadItem } from "@/lib/downloads";
import { GetSourceStatus, RunPluginAction } from "../../wailsjs/go/main/App";
import { SourceStatus } from "./SourceStatus";
import { button, deferred, mount } from "../test/render";
import { act } from "react";
import { toastWithSound as toast } from "@/lib/toast-with-sound";

vi.mock("../../wailsjs/go/main/App", () => ({ GetSourceStatus: vi.fn(), RunPluginAction: vi.fn() }));
vi.mock("@/hooks/useDownloads", () => ({ useDownloads: vi.fn() }));
vi.mock("@/lib/toast-with-sound", () => ({ toastWithSound: { success: vi.fn(), error: vi.fn() } }));

const item = (id: string, status: string, message = "") => ({ id, status, message }) as DownloadItem;
const store = (items: DownloadItem[]) =>
    vi.mocked(useDownloads).mockReturnValue({ items, byTrackId: new Map(), activeCount: 0, guard: {} } as unknown as ReturnType<typeof useDownloads>);
let view: Awaited<ReturnType<typeof mount>>;

beforeEach(() => {
    vi.resetAllMocks();
    vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
    vi.mocked(GetSourceStatus).mockResolvedValue([]);
});
afterEach(async () => { await view.unmount(); vi.unstubAllGlobals(); });

describe("SourceStatus", () => {
    it("reloads when a download changes status, not on every poll", async () => {
        store([item("a", "downloading")]);
        view = await mount(<SourceStatus/>);
        expect(GetSourceStatus).toHaveBeenCalledTimes(1);
        store([item("a", "downloading", "50%")]); // a poll with only a new message
        await view.render(<SourceStatus/>);
        expect(GetSourceStatus).toHaveBeenCalledTimes(1);
        store([item("a", "failed")]);
        await view.render(<SourceStatus/>);
        expect(GetSourceStatus).toHaveBeenCalledTimes(2);
    });

    const site = (name: string, extra: Record<string, unknown> = {}) => ({ name, label: name.toUpperCase(), accepts: "any", available: true, needs_action: false, action: "", reason: "", count: 0, limit: 0, waits: null, wait: 0, asking: false, paused_until: 0, ...extra });
    const needing = [site("alpha", { available: false, needs_action: true, action: "verify" })];

    it("names a site by its label", async () => {
        store([]);
        vi.mocked(GetSourceStatus).mockResolvedValue([site("alpha")] as never);
        view = await mount(<SourceStatus/>);
        expect(view.container.textContent).toContain("ALPHA");
    });

    it("offers the action the site names, runs it for that site, and reloads", async () => {
        store([]);
        const run = deferred<void>();
        vi.mocked(GetSourceStatus).mockResolvedValue(needing as never);
        vi.mocked(RunPluginAction).mockReturnValue(run.promise);
        view = await mount(<SourceStatus/>);
        await act(async () => button(view.container, "Pass check").click());
        expect(RunPluginAction).toHaveBeenCalledWith("alpha", "verify");
        expect(button(view.container, "Pass the check in the browser window…").disabled).toBe(true);
        const calls = vi.mocked(GetSourceStatus).mock.calls.length;
        await act(async () => run.resolve());
        expect(vi.mocked(GetSourceStatus).mock.calls.length).toBeGreaterThan(calls);
        expect(toast.success).toHaveBeenCalledWith("Check passed, ALPHA is back");
    });

    it("shows a failed action's error and re-enables the button", async () => {
        store([]);
        vi.mocked(GetSourceStatus).mockResolvedValue(needing as never);
        vi.mocked(RunPluginAction).mockRejectedValue(new Error("timed out"));
        view = await mount(<SourceStatus/>);
        await act(async () => button(view.container, "Pass check").click());
        expect(toast.error).toHaveBeenCalledWith(expect.stringContaining("timed out"));
        expect(button(view.container, "Pass check").disabled).toBe(false);
    });

    it("offers no button without withAction", async () => {
        store([]);
        vi.mocked(GetSourceStatus).mockResolvedValue(needing as never);
        view = await mount(<SourceStatus withAction={false}/>);
        expect(view.container.textContent).toContain("needs the check");
        expect(view.container.querySelector("button")).toBeNull();
    });

    it("offers no button when the site names no action", async () => {
        store([]);
        vi.mocked(GetSourceStatus).mockResolvedValue([{ ...needing[0], action: "" }] as never);
        view = await mount(<SourceStatus/>);
        expect(view.container.textContent).toContain("needs the check");
        expect(view.container.querySelector("button")).toBeNull();
    });
});
