import { useSyncExternalStore } from "react";
import { en } from "../locales/en";

export type Plural = { zero?: string; one?: string; two?: string; few?: string; many?: string; other: string };
export type Entry = string | Plural;
export type Locale = Record<string, Entry>;
export type MessageKey = Exclude<keyof typeof en, "$name">;
export type Params = Record<string, string | number>;

// Every locales/<code>.json is a language; the file stem is the code.
const files = import.meta.glob<Locale>("../locales/*.json", { eager: true, import: "default" });
export function buildLocales(found: Record<string, Locale>): Record<string, Locale> {
    const locales: Record<string, Locale> = { en };
    for (const [path, locale] of Object.entries(found)) {
        const code = path.match(/([^/]+)\.json$/)![1];
        // A stem that is not a language tag (xx_YY) would make Intl throw later.
        try { Intl.getCanonicalLocales(code); } catch { continue; }
        locales[code] = locale;
    }
    return locales;
}
export const LOCALES = buildLocales(files);

export function availableLanguages(locales: Record<string, Locale> = LOCALES): { code: string; name: string }[] {
    const codes = Object.keys(locales).filter(code => code !== "en").sort();
    return ["en", ...codes].map(code => ({ code, name: String(locales[code]["$name"] ?? code) }));
}

export function resolveLanguage(setting: string, preferred: readonly string[], locales: Record<string, Locale> = LOCALES): string {
    if (Object.hasOwn(locales, setting)) return setting;
    return preferred.map(tag => tag.split("-")[0].toLowerCase()).find(code => Object.hasOwn(locales, code)) ?? "en";
}

export function translate(lang: string, key: MessageKey, params?: Params, locales: Record<string, Locale> = LOCALES): string {
    const entry = pick(locales[lang]?.[key], lang, params) ?? pick(locales.en[key], "en", params) ?? key;
    // {count} gets the language's digit grouping (1,234 / 1 234).
    return entry.replace(/\{(\w+)\}/g, (match, name: string) => (params && name in params ? (name === "count" ? Number(params[name]).toLocaleString(lang) : String(params[name])) : match));
}

function pick(entry: Entry | undefined, lang: string, params?: Params): string | undefined {
    if (typeof entry !== "object") return entry;
    const form = params ? new Intl.PluralRules(lang).select(Number(params.count)) : "other";
    return entry[form] ?? entry.other;
}

let current = "en";
const listeners = new Set<() => void>();

export function setLanguage(setting: string): void {
    current = resolveLanguage(setting, navigator.languages);
    document.documentElement.lang = current;
    listeners.forEach(notify => notify());
}

export const getLanguage = (): string => current;
export const t = (key: MessageKey, params?: Params): string => translate(current, key, params);

const subscribe = (listener: () => void) => {
    listeners.add(listener);
    return () => { listeners.delete(listener); };
};

export function useI18n(): { t: typeof t; language: string } {
    return { t, language: useSyncExternalStore(subscribe, getLanguage, getLanguage) };
}
