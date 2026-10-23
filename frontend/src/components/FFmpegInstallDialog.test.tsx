// @vitest-environment jsdom
import { act } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { closeFFmpegInstall, openFFmpegInstall } from "@/lib/ffmpeg-install";
import { setLanguage } from "@/lib/i18n";
import { updateSettings } from "@/lib/settings";
import { CancelFFmpegInstall, DefaultFFmpegDir, GetFFmpegInstall, SelectFFmpegFolder, StartFFmpegInstall } from "../../wailsjs/go/main/App";
import { FFmpegInstallDialog } from "./FFmpegInstallDialog";
import { change, deferred, mount } from "../test/render";

vi.mock("../../wailsjs/go/main/App", () => ({
    DefaultFFmpegDir: vi.fn(), SelectFFmpegFolder: vi.fn(), StartFFmpegInstall: vi.fn(), GetFFmpegInstall: vi.fn(), CancelFFmpegInstall: vi.fn(),
}));
vi.mock("@/lib/settings", () => ({ updateSettings: vi.fn() }));

const MB = 1024 * 1024;
const status = (extra: Record<string, unknown>) => ({ step: "download", done: 0, total: 0, path: "", error: "", not_empty: false, ...extra }) as Awaited<ReturnType<typeof GetFFmpegInstall>>;
const text = () => document.body.textContent ?? "";
const buttons = (label: string) => [...document.body.querySelectorAll("button")].filter((b) => b.textContent?.trim() === label);
const click = (label: string) => act(async () => buttons(label)[0].click());
const folder = () => document.body.querySelector<HTMLInputElement>("input")!;
const tick = (ms = 250) => act(async () => { await vi.advanceTimersByTimeAsync(ms); });
const onInstalled = vi.fn();
let view: Awaited<ReturnType<typeof mount>>;

const show = async () => {
    view = await mount(<FFmpegInstallDialog/>);
    await act(async () => openFFmpegInstall(onInstalled));
};
const install = async (poll: ReturnType<typeof status>) => {
    vi.mocked(GetFFmpegInstall).mockResolvedValue(poll);
    await click("Install");
    await tick();
};

beforeEach(() => {
    vi.resetAllMocks();
    vi.useFakeTimers();
    vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
    setLanguage("en");
    vi.mocked(DefaultFFmpegDir).mockResolvedValue({ dir: "C:\\App\\ffmpeg", fallback: false });
    vi.mocked(StartFFmpegInstall).mockResolvedValue();
    vi.mocked(CancelFFmpegInstall).mockResolvedValue();
    vi.mocked(updateSettings).mockResolvedValue({} as never);
});
afterEach(async () => {
    await act(async () => closeFFmpegInstall());
    await view.unmount();
    vi.useRealTimers();
    vi.unstubAllGlobals();
});

describe("FFmpegInstallDialog", () => {
    it("asks with the default folder", async () => {
        await show();
        expect(text()).toContain("FFmpeg not found");
        expect(text()).toContain("about 82 MB");
        expect(folder().value).toBe("C:\\App\\ffmpeg");
        expect(text()).not.toContain("Program folder is read-only");
    });

    it("tells that the fallback folder is used", async () => {
        vi.mocked(DefaultFFmpegDir).mockResolvedValue({ dir: "C:\\Users\\M\\AppData\\Local\\Sonar\\ffmpeg", fallback: true });
        await show();
        expect(folder().value).toBe("C:\\Users\\M\\AppData\\Local\\Sonar\\ffmpeg");
        expect(text()).toContain("Program folder is read-only");
    });

    it("Browse sets the folder and a cancelled pick keeps it", async () => {
        await show();
        vi.mocked(SelectFFmpegFolder).mockResolvedValue("D:\\Tools\\ffmpeg");
        await click("Browse");
        expect(folder().value).toBe("D:\\Tools\\ffmpeg");
        vi.mocked(SelectFFmpegFolder).mockResolvedValue("");
        await click("Browse");
        expect(folder().value).toBe("D:\\Tools\\ffmpeg");
    });

    it("Not now closes without installing", async () => {
        await show();
        await click("Not now");
        expect(text()).not.toContain("FFmpeg not found");
        expect(StartFFmpegInstall).not.toHaveBeenCalled();
    });

    it("shows download progress, speed and time left", async () => {
        await show();
        await install(status({ done: 0, total: 82 * MB }));
        vi.mocked(GetFFmpegInstall).mockResolvedValue(status({ done: 34 * MB, total: 82 * MB }));
        await tick(1000);
        expect(StartFFmpegInstall).toHaveBeenCalledWith("C:\\App\\ffmpeg");
        for (const part of ["Downloading FFmpeg", "step 1 of 2", "34 of 82 MB", "MB/s", "left"]) expect(text()).toContain(part);
        expect(buttons("Cancel")).toHaveLength(1);
        expect(document.body.querySelector('[role="progressbar"]')!.getAttribute("aria-valuenow")).toBe("41");
    });

    it("shows the unpacking step", async () => {
        await show();
        await install(status({ step: "install", done: 120, total: 480 }));
        for (const part of ["Installing FFmpeg", "step 2 of 2", "Unpacking: 120 of 480 files"]) expect(text()).toContain(part);
    });

    it("Cancel stops the install and closes", async () => {
        await show();
        await install(status({ done: 1 * MB, total: 82 * MB }));
        await click("Cancel");
        expect(CancelFFmpegInstall).toHaveBeenCalledTimes(1);
        expect(text()).not.toContain("Downloading FFmpeg");
        const polls = vi.mocked(GetFFmpegInstall).mock.calls.length;
        await tick(1000);
        expect(vi.mocked(GetFFmpegInstall).mock.calls.length).toBe(polls);
    });

    it("a Cancel that finds the install already done takes the done path", async () => {
        await show();
        await install(status({ done: 1 * MB, total: 82 * MB }));
        vi.mocked(GetFFmpegInstall).mockResolvedValue(status({ step: "done", path: "C:\\App\\ffmpeg\\ffmpeg.exe" }));
        await click("Cancel");
        await tick(1000);
        expect(updateSettings).toHaveBeenCalledWith({ ffmpegPath: "C:\\App\\ffmpeg\\ffmpeg.exe" });
        expect(onInstalled).toHaveBeenCalledTimes(1);
        expect(text()).toContain("FFmpeg installed");
    });

    it("an install that ended as cancelled just closes", async () => {
        await show();
        await install(status({ step: "error", error: "cancelled" }));
        expect(text()).not.toContain("cancelled");
        expect(text()).not.toContain("FFmpeg");
    });

    it("saves the path, tells the caller once and shows where it went", async () => {
        await show();
        await install(status({ step: "done", path: "C:\\App\\ffmpeg\\bin\\ffmpeg.exe" }));
        await tick(1000);
        expect(updateSettings).toHaveBeenCalledWith({ ffmpegPath: "C:\\App\\ffmpeg\\bin\\ffmpeg.exe" });
        expect(onInstalled).toHaveBeenCalledTimes(1);
        expect(onInstalled).toHaveBeenCalledWith("C:\\App\\ffmpeg\\bin\\ffmpeg.exe");
        expect(text()).toContain("FFmpeg installed");
        expect(text()).toContain("C:\\App\\ffmpeg\\bin\\ffmpeg.exe");
    });

    it("shows the error and Retry starts again", async () => {
        await show();
        await install(status({ step: "error", error: "checksum mismatch" }));
        expect(text()).toContain("checksum mismatch");
        vi.mocked(GetFFmpegInstall).mockResolvedValue(status({ done: 1, total: 2 }));
        await click("Retry");
        await tick();
        expect(StartFFmpegInstall).toHaveBeenCalledTimes(2);
        expect(text()).toContain("Downloading FFmpeg");
    });

    it("a folder that is not empty says so and lets you pick another", async () => {
        await show();
        await install(status({ step: "error", error: "target directory has files", not_empty: true }));
        expect(text()).toContain("The folder is not empty, pick another one");
        expect(text()).not.toContain("target directory has files");
        vi.mocked(SelectFFmpegFolder).mockResolvedValue("E:\\ff\\ffmpeg");
        await click("Browse");
        expect(folder().value).toBe("E:\\ff\\ffmpeg");
    });

    it("a failed start shows the error", async () => {
        const start = deferred<void>();
        vi.mocked(StartFFmpegInstall).mockReturnValue(start.promise);
        await show();
        await click("Install");
        await act(async () => start.reject(new Error("an FFmpeg install is already running")));
        expect(text()).toContain("an FFmpeg install is already running");
        expect(buttons("Retry")).toHaveLength(1);
    });

    it("typing in the folder field changes what is installed", async () => {
        await show();
        await act(async () => change(folder(), "F:\\x"));
        vi.mocked(GetFFmpegInstall).mockResolvedValue(status({}));
        await click("Install");
        expect(StartFFmpegInstall).toHaveBeenCalledWith("F:\\x");
    });
});
