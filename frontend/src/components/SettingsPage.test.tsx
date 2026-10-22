// @vitest-environment jsdom
import { act } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { getLanguage, setLanguage } from "@/lib/i18n";
import { toastWithSound } from "@/lib/toast-with-sound";
import { BrowserOpenURL } from "../../wailsjs/runtime/runtime";
import { SettingsPage } from "./SettingsPage";
import { mount, button, change, deferred } from "../test/render";
import { GetSourceStatus, PassDoubleDoubleCheck, PreviewDownloadPath, SelectDownloadFolder, FindFFmpeg, SelectFFmpeg, DefaultDownloadDir } from "../../wailsjs/go/main/App";

vi.mock("@/components/ui/select", () => import("../test/nativeSelect"));
vi.mock("../../wailsjs/go/main/App", () => ({
    LoadSettings: vi.fn(), SaveSettings: vi.fn(), SelectDownloadFolder: vi.fn(), DefaultDownloadDir: vi.fn(), PreviewDownloadPath: vi.fn(),
    GetSourceStatus: vi.fn(), PassDoubleDoubleCheck: vi.fn(), FindFFmpeg: vi.fn(), SelectFFmpeg: vi.fn(),
}));
vi.mock("../../wailsjs/runtime/runtime", () => ({ BrowserOpenURL: vi.fn() }));
vi.mock("@/lib/toast-with-sound", () => ({ toastWithSound: { success: vi.fn(), error: vi.fn() } }));

const onUnsavedChangesChange = vi.fn();
let view: Awaited<ReturnType<typeof mount>>;

beforeEach(async () => {
    vi.resetAllMocks();
    vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
    vi.mocked(DefaultDownloadDir).mockResolvedValue("C:\\Music\\sonar");
    vi.mocked(PreviewDownloadPath).mockResolvedValue("C:\\Music\\sonar\\Song - Artist.flac");
    vi.mocked(GetSourceStatus).mockResolvedValue([{ name: "lucida", available: true, needs_check: false, reason: "" }, { name: "doubledouble", available: false, needs_check: true, reason: "" }]);
    vi.mocked(FindFFmpeg).mockResolvedValue("");
    vi.mocked(SelectFFmpeg).mockResolvedValue("C:\\ff\\ffmpeg.exe");
    Object.defineProperty(window, "matchMedia", { configurable: true, value: () => ({ matches: false, addEventListener: vi.fn(), removeEventListener: vi.fn() }) });
    view = await mount(<SettingsPage onUnsavedChangesChange={onUnsavedChangesChange}/>);
});
afterEach(async () => { await view.unmount(); setLanguage("en"); vi.unstubAllGlobals(); });

describe("SettingsPage", () => {
    it("shows a section per nav button, starting with General", async () => {
        expect([...view.container.querySelectorAll(".border-b.shrink-0 button")].map((item) => item.textContent)).toEqual(["General", "Downloads", "Appearance", "Network"]);
        expect(view.container.textContent).toContain("Language");
        expect(view.container.textContent).toContain("Sound Effects");
        expect(view.container.querySelector('input[placeholder="Auto (system proxy)"]')).toBeNull();

        await act(async () => button(view.container, "Network").click());
        expect(view.container.querySelector('input[placeholder="Auto (system proxy)"]')).not.toBeNull();
        expect(view.container.textContent).not.toContain("Sound Effects");
    });

    it("switching sections alone does not mark the page unsaved", async () => {
        await act(async () => button(view.container, "Appearance").click());
        await act(async () => button(view.container, "Network").click());
        expect(onUnsavedChangesChange).not.toHaveBeenCalledWith(true);
    });

    it("previews a picked language at once and marks the page unsaved", async () => {
        const select = view.container.querySelector("select")!;
        expect([...select.options].map((option) => option.value)).toEqual(["auto", "en", "ru"]);
        await act(async () => {
            select.value = "ru";
            select.dispatchEvent(new Event("change", { bubbles: true }));
        });
        expect(getLanguage()).toBe("ru");
        expect(onUnsavedChangesChange).toHaveBeenLastCalledWith(true);
    });

    it("shows the accent colour names in the picked language", async () => {
        const languageSelect = view.container.querySelector("select")!;
        await act(async () => {
            languageSelect.value = "ru";
            languageSelect.dispatchEvent(new Event("change", { bubbles: true }));
        });
        await act(async () => button(view.container, "Оформление").click());
        const accent = [...view.container.querySelectorAll("select")].find((select) => select.querySelector('option[value="blue"]'))!;
        const labels = [...accent.options].map((option) => option.textContent);
        expect(labels).toContain("Синий");
        expect(labels).toContain("Красный");
        expect(labels).not.toContain("Blue");
    });

    describe("Downloads", () => {
        const open = () => act(async () => button(view.container, "Downloads").click());
        const selectWith = (value: string) => [...view.container.querySelectorAll("select")].find((select) => select.querySelector(`option[value="${value}"]`))!;
        const pick = (select: HTMLSelectElement, value: string) => act(async () => {
            select.value = value;
            select.dispatchEvent(new Event("change", { bubbles: true }));
        });
        const dirInput = () => [...view.container.querySelectorAll("input")].find((input) => input.placeholder === "C:\\Music\\sonar");
        const lastOptions = () => vi.mocked(PreviewDownloadPath).mock.lastCall![0];

        it("is the second tab and shows its settings", async () => {
            await open();
            for (const text of ["Download folder", "Source", "Format", "Folder structure", "File name", "Playlist folder", "Preview", "C:\\Music\\sonar\\Song - Artist.flac"])
                expect(view.container.textContent).toContain(text);
            expect(dirInput()).toBeDefined();
            expect(view.container.textContent).not.toContain("Found automatically");
        });

        it("Browse puts the chosen folder in the field and marks the page unsaved", async () => {
            vi.mocked(SelectDownloadFolder).mockResolvedValue("D:\\Music");
            await open();
            await act(async () => button(view.container, "Browse").click());
            expect(SelectDownloadFolder).toHaveBeenCalledWith("C:\\Music\\sonar");
            expect(dirInput()!.value).toBe("D:\\Music");
            expect(onUnsavedChangesChange).toHaveBeenLastCalledWith(true);
        });

        it("a custom template shows an input", async () => {
            await open();
            await pick(selectWith("{artist} - {title}"), "custom");
            const input = view.container.querySelector('input[value="{title} - {artist}"]') as HTMLInputElement;
            await act(async () => change(input, "{artist}/{title}"));
            expect(lastOptions()).toMatchObject({ filename_template: "{artist}/{title}" });
        });

        it("doubledouble that needs the check offers the button", async () => {
            const check = deferred<void>();
            vi.mocked(PassDoubleDoubleCheck).mockReturnValue(check.promise);
            await open();
            expect(view.container.textContent).toContain("needs the check");
            const calls = vi.mocked(GetSourceStatus).mock.calls.length;
            await act(async () => button(view.container, "Pass check").click());
            expect(PassDoubleDoubleCheck).toHaveBeenCalled();
            expect(button(view.container, "Pass the check in the browser window…").disabled).toBe(true);
            expect(view.container.textContent).toContain("untick “Upload to”, tick “Hide my download”");
            await act(async () => check.resolve());
            expect(view.container.textContent).not.toContain("untick “Upload to”");
            expect(vi.mocked(GetSourceStatus).mock.calls.length).toBeGreaterThan(calls);
            expect(vi.mocked(toastWithSound.success)).toHaveBeenCalledWith("Check passed, doubledouble is back");
        });

        it("a failed check shows the error and re-enables the button", async () => {
            vi.mocked(PassDoubleDoubleCheck).mockRejectedValue(new Error("timed out"));
            await open();
            const calls = vi.mocked(GetSourceStatus).mock.calls.length;
            await act(async () => button(view.container, "Pass check").click());
            expect(vi.mocked(GetSourceStatus).mock.calls.length).toBeGreaterThan(calls);
            expect(vi.mocked(toastWithSound.error)).toHaveBeenCalledWith(expect.stringContaining("timed out"));
            expect(button(view.container, "Pass check").disabled).toBe(false);
        });

        it("MP3 shows the FFmpeg row", async () => {
            await open();
            expect(view.container.textContent).not.toContain("Converts the downloaded FLAC");
            await pick(selectWith("mp3-320"), "mp3-320");
            expect(view.container.textContent).toContain("FFmpeg not found, MP3 and M4A won't work");
            expect(button(view.container, "Download FFmpeg")).toBeTruthy();
            await act(async () => [...view.container.querySelectorAll("button")].filter((item) => item.textContent === "Browse")[1].click());
            expect(SelectDownloadFolder).not.toHaveBeenCalled();
            expect([...view.container.querySelectorAll("input")].some((input) => input.value === "C:\\ff\\ffmpeg.exe")).toBe(true);
            expect(lastOptions()).toMatchObject({ ffmpeg_path: "C:\\ff\\ffmpeg.exe", format: "mp3-320" });
        });

        it("the Download FFmpeg link opens ffmpeg.org", async () => {
            await open();
            await pick(selectWith("mp3-320"), "mp3-320");
            await act(async () => button(view.container, "Download FFmpeg").click());
            expect(BrowserOpenURL).toHaveBeenCalledWith("https://ffmpeg.org/download.html");
        });

        it("shows where FFmpeg was found", async () => {
            vi.mocked(FindFFmpeg).mockResolvedValue("C:\\bin\\ffmpeg.exe");
            await open();
            await pick(selectWith("mp3-320"), "mp3-320");
            expect(view.container.textContent).toContain("Found: C:\\bin\\ffmpeg.exe");
            expect(view.container.textContent).not.toContain("FFmpeg not found");
        });

        it("a failed Browse shows the error", async () => {
            vi.mocked(SelectDownloadFolder).mockRejectedValue(new Error("dialog failed"));
            await open();
            await act(async () => button(view.container, "Browse").click());
            expect(vi.mocked(toastWithSound.error)).toHaveBeenCalledWith(expect.stringContaining("dialog failed"));
        });

        it("No subfolder saves an empty folder template", async () => {
            await open();
            await pick(selectWith("{artist}"), "{artist}");
            expect(lastOptions()).toMatchObject({ folder_template: "{artist}" });
            await pick(selectWith("none"), "none");
            expect(lastOptions()).toMatchObject({ folder_template: "" });
        });

        it("opening the tab alone does not mark the page unsaved", async () => {
            await open();
            expect(onUnsavedChangesChange).not.toHaveBeenCalledWith(true);
        });

        it("the rate guard limit goes into the options", async () => {
            await open();
            const limit = view.container.querySelector('input[type="number"]') as HTMLInputElement;
            expect(limit.min).toBe("1");
            await act(async () => change(limit, "30"));
            expect(lastOptions()).toMatchObject({ guard_limit: 30 });
            await act(async () => change(limit, ""));
            expect(lastOptions()).toMatchObject({ guard_limit: 30 });
            await act(async () => limit.dispatchEvent(new FocusEvent("focusout", { bubbles: true })));
            expect(limit.value).toBe("30");
            await act(async () => change(limit, "99999999999999999999"));
            expect(lastOptions()).toMatchObject({ guard_limit: 30 });
            await act(async () => (view.container.querySelector("#rate-guard") as HTMLButtonElement).click());
            expect(view.container.querySelector('input[type="number"]')).toBeNull();
            expect(lastOptions()).toMatchObject({ guard_limit: 0 });
        });
    });
});
