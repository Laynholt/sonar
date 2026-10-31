// @vitest-environment jsdom
import { act } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { getLanguage, setLanguage } from "@/lib/i18n";
import { downloadOptions, type Settings } from "@/lib/settings";
import { openFFmpegInstall } from "@/lib/ffmpeg-install";
import { toastWithSound } from "@/lib/toast-with-sound";
import { BrowserOpenURL, EventsOn } from "../../wailsjs/runtime/runtime";
import { SettingsPage } from "./SettingsPage";
import { mount, button, change } from "../test/render";
import { GetSourceStatus, SaveSettings, SelectDownloadFolder, FindFFmpeg, SelectFFmpeg, DefaultDownloadDir } from "../../wailsjs/go/main/App";

vi.mock("@/components/ui/select", () => import("../test/nativeSelect"));
vi.mock("../../wailsjs/go/main/App", () => ({
    LoadSettings: vi.fn(), SaveSettings: vi.fn(), SelectDownloadFolder: vi.fn(), DefaultDownloadDir: vi.fn(),
    GetSourceStatus: vi.fn(), RunPluginAction: vi.fn(), FindFFmpeg: vi.fn(), SelectFFmpeg: vi.fn(),
}));
vi.mock("@/lib/ffmpeg-install", () => ({ openFFmpegInstall: vi.fn() }));
vi.mock("../../wailsjs/runtime/runtime", () => ({ BrowserOpenURL: vi.fn(), EventsOn: vi.fn() }));
vi.mock("@/lib/toast-with-sound", () => ({ toastWithSound: { success: vi.fn(), error: vi.fn() } }));

const onUnsavedChangesChange = vi.fn();
let view: Awaited<ReturnType<typeof mount>>;

beforeEach(async () => {
    vi.resetAllMocks();
    vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
    vi.mocked(DefaultDownloadDir).mockResolvedValue("C:\\Music\\sonar");
    vi.mocked(GetSourceStatus).mockResolvedValue([{ name: "lucida", label: "Lucida", accepts: "amazon", available: true, needs_action: false, action: "", reason: "", count: 0, limit: 0, waits: [], wait: 0, asking: false, paused_until: 0 }, { name: "doubledouble", label: "DoubleDouble", accepts: "amazon", available: false, needs_action: true, action: "check", reason: "", count: 0, limit: 0, waits: [], wait: 0, asking: false, paused_until: 0 }]);
    vi.mocked(FindFFmpeg).mockResolvedValue("");
    vi.mocked(SelectFFmpeg).mockResolvedValue("C:\\ff\\ffmpeg.exe");
    Object.defineProperty(window, "matchMedia", { configurable: true, value: () => ({ matches: false, addEventListener: vi.fn(), removeEventListener: vi.fn() }) });
    view = await mount(<SettingsPage onUnsavedChangesChange={onUnsavedChangesChange}/>);
});
afterEach(async () => { await view.unmount(); setLanguage("en"); vi.unstubAllGlobals(); });

describe("SettingsPage", () => {
    it("shows a section per nav button, starting with General", async () => {
        expect([...view.container.querySelectorAll(".border-b.shrink-0 button")].map((item) => item.textContent)).toEqual(["General", "Downloads", "Appearance", "Network", "Plugins"]);
        expect(view.container.textContent).toContain("Language");
        expect(view.container.textContent).toContain("Sound Effects");
        expect(view.container.querySelector('input[placeholder="Auto (system proxy)"]')).toBeNull();

        await act(async () => button(view.container, "Network").click());
        expect(view.container.querySelector('input[placeholder="Auto (system proxy)"]')).not.toBeNull();
        expect(view.container.textContent).not.toContain("Sound Effects");
    });

    it("saves the recent limit and ignores values over the cap", async () => {
        const limit = view.container.querySelector("#recent-limit") as HTMLInputElement;
        expect(limit.value).toBe("20");
        await act(async () => change(limit, "8"));
        await act(async () => change(limit, "201"));
        await act(async () => button(view.container, "Save Changes").click());
        expect(vi.mocked(SaveSettings).mock.lastCall![0]).toMatchObject({ recentLimit: 8 });
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
        // Saves, then reads the download options from what reached the backend.
        const lastOptions = async () => {
            await act(async () => button(view.container, "Save Changes").click());
            return downloadOptions(vi.mocked(SaveSettings).mock.lastCall![0] as unknown as Settings);
        };

        it("is the second tab and shows its settings", async () => {
            await open();
            expect([...view.container.querySelectorAll("h2")].map((item) => item.textContent)).toEqual(["Saving", "Folders and file names", "Source"]);
            for (const text of ["Download folder", "Site", "Site status", "Format", "Folder structure", "File name", "Playlist folder"])
                expect(view.container.textContent).toContain(text);
            expect(view.container.textContent).not.toContain("Preview");
            expect(dirInput()).toBeDefined();
            expect(view.container.textContent).not.toContain("Found automatically");
        });

        it("the guard and genre controls are gone, and so is the check button", async () => {
            await open();
            for (const text of ["Write genre", "Only one genre", "Download limit warning", "Tracks before the warning", "Pause length"])
                expect(view.container.textContent).not.toContain(text);
            expect(view.container.querySelector("#embed-genre, #single-genre, #rate-guard")).toBeNull();
            expect(view.container.querySelector('input[type="number"]')).toBeNull();
            // The status line stays; only the way to pass the check moved out.
            expect(view.container.textContent).toContain("needs the check");
            expect([...view.container.querySelectorAll("button")].some((b) => b.textContent === "Pass check")).toBe(false);
        });

        // The saved settings outlive a test, so each one that picks a source puts Auto back.
        const choose = async (value: string) => { await pick(selectWith(value), value); await lastOptions(); };

        it("the Source select lists Auto and the enabled downloaders by name, and saves the pick", async () => {
            await open();
            await choose("auto");
            const source = selectWith("auto");
            expect([...source.options].map((option) => [option.value, option.textContent])).toEqual([["auto", "Auto"], ["lucida", "Lucida"], ["doubledouble", "DoubleDouble"]]);
            await pick(source, "doubledouble");
            expect(await lastOptions()).toMatchObject({ source: "doubledouble" });
            await choose("auto");
        });

        it("reloads the downloaders when a plugin changes", async () => {
            await open();
            await choose("auto");
            vi.mocked(GetSourceStatus).mockResolvedValue([]);
            await act(async () => vi.mocked(EventsOn).mock.calls[0][1]());
            expect([...selectWith("auto").options].map((option) => option.value)).toEqual(["auto"]);
        });

        it("keeps a chosen downloader that was switched off in the list instead of changing the choice", async () => {
            await open();
            await choose("lucida");
            vi.mocked(GetSourceStatus).mockResolvedValue([]);
            await act(async () => vi.mocked(EventsOn).mock.calls[0][1]());
            const source = selectWith("auto");
            expect([...source.options].map((option) => option.value)).toEqual(["auto", "lucida"]);
            expect(source.value).toBe("lucida");
            await choose("auto");
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
            expect(await lastOptions()).toMatchObject({ filename_template: "{artist}/{title}" });
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
            expect(await lastOptions()).toMatchObject({ ffmpeg_path: "C:\\ff\\ffmpeg.exe", format: "mp3-320" });
        });

        it("choosing MP3 without FFmpeg offers the install and uses the installed path", async () => {
            await open();
            await pick(selectWith("mp3-320"), "mp3-320");
            expect(openFFmpegInstall).toHaveBeenCalledTimes(1);
            await act(async () => vi.mocked(openFFmpegInstall).mock.calls[0][0]!("C:\\ff\\ffmpeg.exe"));
            expect(await lastOptions()).toMatchObject({ ffmpeg_path: "C:\\ff\\ffmpeg.exe", format: "mp3-320" });
        });

        it("choosing MP3 with FFmpeg found does not open the install", async () => {
            vi.mocked(FindFFmpeg).mockResolvedValue("C:\\bin\\ffmpeg.exe");
            await open();
            await pick(selectWith("mp3-320"), "mp3-320");
            expect(openFFmpegInstall).not.toHaveBeenCalled();
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
            // An earlier test may have saved a custom path; the found one shows in an empty field.
            const input = view.container.querySelector<HTMLInputElement>("#ffmpeg-path")!;
            await act(async () => change(input, ""));
            expect(input.placeholder).toBe("C:\\bin\\ffmpeg.exe");
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
            expect(await lastOptions()).toMatchObject({ folder_template: "{artist}" });
            await pick(selectWith("none"), "none");
            expect(await lastOptions()).toMatchObject({ folder_template: "" });
        });

        it("opening the tab alone does not mark the page unsaved", async () => {
            await open();
            expect(onUnsavedChangesChange).not.toHaveBeenCalledWith(true);
        });
    });
});
