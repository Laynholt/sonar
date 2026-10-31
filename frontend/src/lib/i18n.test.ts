// @vitest-environment jsdom
import { afterEach, expect, it } from "vitest";
import { en } from "../locales/en";
import ru from "../locales/ru.json";
import { LOCALES, availableLanguages, buildLocales, getLanguage, resolveLanguage, setLanguage, translate } from "./i18n";

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

it("skips a locale file whose name is not a language tag", () => {
    const built = buildLocales({ "../locales/xx_YY.json": { "$name": "Bad" }, "../locales/de.json": { "$name": "Deutsch" } });
    expect(Object.keys(built).sort()).toEqual(["de", "en"]);
});

it("names a language by its code when the file has no $name", () => {
    expect(availableLanguages({ en, de: {} })).toContainEqual({ code: "de", name: "de" });
});

it("does not mistake Object.prototype names for languages", () => {
    expect(resolveLanguage("constructor", ["toString"])).toBe("en");
});

it("keeps the Russian copy the mockups approved", () => {
    const ru = (key: Parameters<typeof translate>[1], params?: Record<string, number | string>) => translate("ru", key, params);
    expect(ru("picker.title")).toBe("Неуверенное совпадение");
    expect(ru("picker.from", { service: "Spotify" })).toBe("Из Spotify");
    expect(ru("picker.found", { service: "Amazon" })).toBe("Нашлось на Amazon");
    expect(ru("picker.recommended")).toBe("Рекомендован");
    expect(ru("picker.choose")).toBe("Выбрать");
    expect(ru("picker.skip")).toBe("Пропустить трек");
    expect(ru("picker.position", { n: 1, total: 3 })).toBe("1 из 3 ждут решения");
    expect(ru("picker.next")).toBe("следующий →");
    expect(ru("picker.none")).toBe("Ничего не нашлось");
    expect(ru("picker.pasteLink")).toBe("Нет нужного? Вставить ссылку");
    expect(ru("download.status.review")).toBe("Нужно решение");
    expect(ru("download.choose")).toBe("Выбрать…");
    expect(ru("download.tabReview")).toBe("Ждут решения");
    expect(ru("download.status.matching")).toBe("Ищем совпадение…");
    expect(ru("widget.review", { count: 1 })).toBe("⚠ 1 трек ждёт твоего решения");
    expect(ru("widget.review", { count: 3 })).toBe("⚠ 3 трека ждут твоего решения");
    expect(ru("widget.review", { count: 5 })).toBe("⚠ 5 треков ждут твоего решения");
    expect(ru("widget.reviewOpen")).toBe("Решить");
    expect(ru("widget.siteBusy", { count: 12, limit: 50 })).toBe("качает · 12/50");
    expect(ru("widget.sitePaused", { time: "24:13" })).toBe("пауза · 24:13");
    expect(ru("download.resume")).toBe("Продолжить");
    expect(ru("widget.siteReady")).toBe("готов");
});
