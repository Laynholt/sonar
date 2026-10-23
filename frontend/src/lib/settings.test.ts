import { beforeEach, afterEach, expect, it, vi } from "vitest";

vi.mock("../../wailsjs/go/main/App", () => ({
    LoadSettings: vi.fn(), SaveSettings: vi.fn(),
}));

beforeEach(() => {
    vi.resetModules();
    vi.resetAllMocks();
    const values = new Map<string, string>();
    vi.stubGlobal("localStorage", {
        getItem: (key: string) => values.get(key) ?? null,
        setItem: (key: string, value: string) => values.set(key, value),
    });
    vi.stubGlobal("window", { dispatchEvent: vi.fn() });
    vi.stubGlobal("CustomEvent", class { constructor(public type: string, public options: unknown) {} });
});
afterEach(() => vi.unstubAllGlobals());

it("keeps saved settings and notifications unchanged until the backend confirms the write", async () => {
    const api = await import("../../wailsjs/go/main/App");
    const settings = await import("./settings");
    const original = { ...settings.DEFAULT_SETTINGS, theme: "blue" };
    await settings.saveSettings(original);
    const stored = localStorage.getItem("sonar-settings");
    const events = vi.mocked(window.dispatchEvent).mock.calls.length;
    let reject!: (error: Error) => void;
    vi.mocked(api.SaveSettings).mockReturnValue(new Promise((_resolve, fail) => { reject = fail; }));
    const failure = new Error("disk full");
    const saving = settings.saveSettings({ ...original, theme: "red" });
    expect(settings.getSettings().theme).toBe("blue");
    expect(localStorage.getItem("sonar-settings")).toBe(stored);
    reject(failure);
    await expect(saving).rejects.toBe(failure);
    expect(settings.getSettings().theme).toBe("blue");
    expect(localStorage.getItem("sonar-settings")).toBe(stored);
    expect(window.dispatchEvent).toHaveBeenCalledTimes(events);
});

it("commits confirmed settings", async () => {
    const settings = await import("./settings");
    await settings.saveSettings({ ...settings.DEFAULT_SETTINGS, theme: "green" });
    expect(settings.getSettings().theme).toBe("green");
    expect(JSON.parse(localStorage.getItem("sonar-settings")!)).toMatchObject({ theme: "green" });
    expect(window.dispatchEvent).toHaveBeenCalledTimes(1);
});

it("falls back to defaults when the backend fails", async () => {
    const api = await import("../../wailsjs/go/main/App");
    const settings = await import("./settings");
    const failure = new Error("invalid character '}' in config.json");
    vi.mocked(api.LoadSettings).mockRejectedValue(failure);
    await expect(settings.loadSettings()).resolves.toEqual(settings.DEFAULT_SETTINGS);
    // An unreadable config must never be overwritten, and the next load retries.
    expect(api.SaveSettings).not.toHaveBeenCalled();
    expect(settings.getSettingsLoadError()).toBe(failure);

    vi.mocked(api.LoadSettings).mockResolvedValue({ theme: "violet" });
    expect((await settings.loadSettings()).theme).toBe("violet");
    expect(settings.getSettingsLoadError()).toBeNull();
});

it("fills defaults when the backend has nothing saved", async () => {
    const api = await import("../../wailsjs/go/main/App");
    const settings = await import("./settings");
    vi.mocked(api.LoadSettings).mockResolvedValue({});
    expect(await settings.loadSettings()).toEqual(settings.DEFAULT_SETTINGS);
    expect(api.SaveSettings).not.toHaveBeenCalled();
});

it("keeps proxy across save and load", async () => {
    const api = await import("../../wailsjs/go/main/App");
    const settings = await import("./settings");
    await settings.updateSettings({ proxy: "socks5://127.0.0.1:10808" });
    // The backend treats a missing proxy as "", so the full object must be sent.
    expect(api.SaveSettings).toHaveBeenCalledWith({ ...settings.DEFAULT_SETTINGS, proxy: "socks5://127.0.0.1:10808" });
    const saved = vi.mocked(api.SaveSettings).mock.calls[0][0];

    vi.resetModules();
    const reloaded = await import("./settings");
    vi.mocked(api.LoadSettings).mockResolvedValue(saved);
    expect((await reloaded.loadSettings()).proxy).toBe("socks5://127.0.0.1:10808");
});

it("defaults the language to auto and keeps a saved one", async () => {
    const api = await import("../../wailsjs/go/main/App");
    const settings = await import("./settings");
    expect(settings.DEFAULT_SETTINGS.language).toBe("auto");
    vi.mocked(api.LoadSettings).mockResolvedValue({ language: "ru" });
    expect((await settings.loadSettings()).language).toBe("ru");
});

it("fills the download defaults for old saved settings and turns the guard off as limit 0", async () => {
    const api = await import("../../wailsjs/go/main/App");
    const settings = await import("./settings");
    vi.mocked(api.LoadSettings).mockResolvedValue({ theme: "blue" });
    const loaded = await settings.loadSettings();
    expect(loaded).toMatchObject({ theme: "blue", filenameTemplate: "{title} - {artist}", rateGuard: true, rateGuardLimit: 50, downloadFormat: "original" });
    expect(settings.downloadOptions(loaded)).toMatchObject({ dir: "", filename_template: "{title} - {artist}", playlist_folder: true, guard_limit: 50 });
    expect(settings.downloadOptions({ ...loaded, rateGuard: false }).guard_limit).toBe(0);
});

it("embeds the genre by default, all genres, and passes both switches to the download options", async () => {
    const settings = await import("./settings");
    expect(settings.DEFAULT_SETTINGS).toMatchObject({ embedGenre: true, useSingleGenre: false });
    expect(settings.downloadOptions(settings.DEFAULT_SETTINGS)).toMatchObject({ embed_genre: true, single_genre: false });
    expect(settings.downloadOptions({ ...settings.DEFAULT_SETTINGS, embedGenre: false, useSingleGenre: true })).toMatchObject({ embed_genre: false, single_genre: true });
});
