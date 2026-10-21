// @vitest-environment jsdom
import { afterEach, expect, it } from "vitest";
import { en } from "../locales/en";
import ru from "../locales/ru.json";
import { LOCALES, availableLanguages, getLanguage, resolveLanguage, setLanguage, translate } from "./i18n";

afterEach(() => setLanguage("en"));

it("falls back to English for a key missing in the language", () => {
    expect(translate("xx", "count.tracks", { count: 2 }, { en, xx: { "$name": "X" } })).toBe("2 tracks");
});

it("falls back to English for an unknown language", () => {
    expect(translate("zz", "service.open", { service: "Spotify" })).toBe("Open in Spotify");
});

it("picks English plural forms", () => {
    expect(translate("en", "count.tracks", { count: 1 })).toBe("1 track");
    expect(translate("en", "count.tracks", { count: 5 })).toBe("5 tracks");
});

it("picks Russian plural forms", () => {
    const forms = [1, 3, 5, 11, 21].map(count => translate("ru", "count.tracks", { count }));
    expect(forms).toEqual(["1 трек", "3 трека", "5 треков", "11 треков", "21 трек"]);
    expect(LOCALES.ru).toBeDefined();
});

it("leaves unknown placeholders as they are", () => {
    expect(translate("en", "service.open", {})).toBe("Open in {service}");
});

it("resolves the language from the setting and the preferred list", () => {
    expect(resolveLanguage("auto", ["ru-RU", "en"])).toBe("ru");
    expect(resolveLanguage("auto", ["de-DE"])).toBe("en");
    expect(resolveLanguage("ru", ["en-US"])).toBe("ru");
    expect(resolveLanguage("de", ["ru-RU"])).toBe("ru");
});

it("lists English first and then the other languages", () => {
    const languages = availableLanguages();
    expect(languages[0]).toEqual({ code: "en", name: "English" });
    expect(languages).toContainEqual({ code: "ru", name: "Русский" });
});

it("keeps ru.json in step with en", () => {
    expect(Object.keys(ru).sort()).toEqual(Object.keys(en).sort());
});

it("applies the language to the store and the document", () => {
    setLanguage("ru");
    expect(getLanguage()).toBe("ru");
    expect(document.documentElement.lang).toBe("ru");
});
