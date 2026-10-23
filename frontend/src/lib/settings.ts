import { LoadSettings, SaveSettings as SaveToBackend } from "../../wailsjs/go/main/App";
import type { download } from "../../wailsjs/go/models";

export type FontFamily = "google-sans" | "inter" | "poppins" | "roboto" | "dm-sans" | "plus-jakarta-sans" | "manrope" | "space-grotesk" | "noto-sans" | "nunito-sans" | "figtree" | "raleway" | "public-sans" | "outfit" | "jetbrains-mono" | "geist-sans" | "bricolage-grotesque";

export interface Settings {
    theme: string;
    themeMode: "auto" | "light" | "dark";
    fontFamily: FontFamily;
    sfxEnabled: boolean;
    proxy: string;
    language: string;
    downloadPath: string;
    downloadFormat: "original" | "mp3-320" | "m4a-aac-320";
    downloadSource: "auto" | "lucida" | "doubledouble";
    folderTemplate: string;
    filenameTemplate: string;
    playlistFolder: boolean;
    embedGenre: boolean;
    useSingleGenre: boolean;
    ffmpegPath: string;
    rateGuard: boolean;
    rateGuardLimit: number;
    rateGuardWait: 15 | 30 | 60 | 120;
    recentLimit: number;
}

export const DEFAULT_SETTINGS: Settings = {
    theme: "yellow",
    themeMode: "auto",
    fontFamily: "google-sans",
    sfxEnabled: true,
    proxy: "",
    language: "auto",
    downloadPath: "",
    downloadFormat: "original",
    downloadSource: "auto",
    folderTemplate: "",
    filenameTemplate: "{title} - {artist}",
    playlistFolder: true,
    embedGenre: true,
    useSingleGenre: false,
    ffmpegPath: "",
    rateGuard: true,
    rateGuardLimit: 50,
    rateGuardWait: 30,
    recentLimit: 20,
};

/** The backend's download options; a guard_limit of 0 means the rate guard is off. */
export function downloadOptions(s: Settings): download.Options {
    return {
        dir: s.downloadPath,
        format: s.downloadFormat,
        source: s.downloadSource,
        folder_template: s.folderTemplate,
        filename_template: s.filenameTemplate,
        playlist_folder: s.playlistFolder,
        ffmpeg_path: s.ffmpegPath,
        guard_limit: s.rateGuard ? s.rateGuardLimit : 0,
        guard_wait: s.rateGuardWait,
        embed_genre: s.embedGenre,
        single_genre: s.useSingleGenre,
    };
}

export const FONT_OPTIONS: {
    value: FontFamily;
    label: string;
    fontFamily: string;
}[] = [
    { value: "bricolage-grotesque", label: "Bricolage Grotesque", fontFamily: '"Bricolage Grotesque", system-ui, sans-serif' },
    { value: "dm-sans", label: "DM Sans", fontFamily: '"DM Sans", system-ui, sans-serif' },
    { value: "figtree", label: "Figtree", fontFamily: '"Figtree", system-ui, sans-serif' },
    { value: "geist-sans", label: "Geist Sans", fontFamily: '"Geist", system-ui, sans-serif' },
    { value: "google-sans", label: "Google Sans", fontFamily: '"Google Sans", system-ui, sans-serif' },
    { value: "inter", label: "Inter", fontFamily: '"Inter", system-ui, sans-serif' },
    { value: "jetbrains-mono", label: "JetBrains Mono", fontFamily: '"JetBrains Mono", ui-monospace, monospace' },
    { value: "manrope", label: "Manrope", fontFamily: '"Manrope", system-ui, sans-serif' },
    { value: "noto-sans", label: "Noto Sans", fontFamily: '"Noto Sans", system-ui, sans-serif' },
    { value: "nunito-sans", label: "Nunito Sans", fontFamily: '"Nunito Sans", system-ui, sans-serif' },
    { value: "outfit", label: "Outfit", fontFamily: '"Outfit", system-ui, sans-serif' },
    { value: "plus-jakarta-sans", label: "Plus Jakarta Sans", fontFamily: '"Plus Jakarta Sans", system-ui, sans-serif' },
    { value: "poppins", label: "Poppins", fontFamily: '"Poppins", system-ui, sans-serif' },
    { value: "public-sans", label: "Public Sans", fontFamily: '"Public Sans", system-ui, sans-serif' },
    { value: "raleway", label: "Raleway", fontFamily: '"Raleway", system-ui, sans-serif' },
    { value: "roboto", label: "Roboto", fontFamily: '"Roboto", system-ui, sans-serif' },
    { value: "space-grotesk", label: "Space Grotesk", fontFamily: '"Space Grotesk", system-ui, sans-serif' },
];
export function applyFont(fontFamily: FontFamily): void {
    const font = FONT_OPTIONS.find(f => f.value === fontFamily);
    if (font) {
        document.documentElement.style.setProperty('--font-sans', font.fontFamily);
        document.body.style.fontFamily = font.fontFamily;
    }
}

const SETTINGS_KEY = "sonar-settings";
let cachedSettings: Settings | null = null;
let hasLoadedSettings = false;
let loadSettingsPromise: Promise<Settings> | null = null;

function normalizeSettingsData(source: Partial<Settings> | null | undefined): Settings {
    return { ...DEFAULT_SETTINGS, ...source };
}

function getSettingsFromLocalStorage(): Settings {
    try {
        const stored = localStorage.getItem(SETTINGS_KEY);
        if (stored) {
            return normalizeSettingsData(JSON.parse(stored));
        }
    }
    catch (error) {
        console.error("Failed to load settings from local storage:", error);
    }
    return DEFAULT_SETTINGS;
}

export function getSettings(): Settings {
    if (cachedSettings)
        return cachedSettings;
    return getSettingsFromLocalStorage();
}

let settingsLoadError: unknown = null;
/** The last error reading config.json; null once it was read or never failed. */
export function getSettingsLoadError(): unknown {
    return settingsLoadError;
}

export async function loadSettings(): Promise<Settings> {
    if (hasLoadedSettings && cachedSettings) {
        return cachedSettings;
    }
    if (loadSettingsPromise) {
        return loadSettingsPromise;
    }
    loadSettingsPromise = (async () => {
        try {
            cachedSettings = normalizeSettingsData(await LoadSettings());
        }
        catch (error) {
            // An unreadable config.json must not be overwritten: the user may fix it.
            // Use the local copy for this session and leave hasLoadedSettings false so the next load retries.
            console.error("Failed to load settings from backend:", error);
            settingsLoadError = error;
            return getSettingsFromLocalStorage();
        }
        settingsLoadError = null;
        hasLoadedSettings = true;
        return cachedSettings;
    })();
    try {
        return await loadSettingsPromise;
    }
    finally {
        loadSettingsPromise = null;
    }
}

export async function saveSettings(settings: Settings): Promise<void> {
    // The backend reads a missing proxy as "", so always send the full object.
    const safeSettings = normalizeSettingsData(settings);
    await SaveToBackend({ ...safeSettings });
    cachedSettings = safeSettings;
    hasLoadedSettings = true;
    try {
        localStorage.setItem(SETTINGS_KEY, JSON.stringify(safeSettings));
    }
    catch (error) {
        console.error("Failed to cache saved settings:", error);
    }
    window.dispatchEvent(new CustomEvent('settingsUpdated', { detail: safeSettings }));
}

export async function updateSettings(partial: Partial<Settings>): Promise<Settings> {
    const current = await loadSettings();
    const updated = { ...current, ...partial };
    await saveSettings(updated);
    return updated;
}

export function applyThemeMode(mode: "auto" | "light" | "dark"): void {
    if (mode === "auto") {
        const prefersDark = window.matchMedia("(prefers-color-scheme: dark)").matches;
        if (prefersDark) {
            document.documentElement.classList.add("dark");
        }
        else {
            document.documentElement.classList.remove("dark");
        }
    }
    else if (mode === "dark") {
        document.documentElement.classList.add("dark");
    }
    else {
        document.documentElement.classList.remove("dark");
    }
}
