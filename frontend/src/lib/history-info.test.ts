// @vitest-environment jsdom
import { afterEach, expect, it } from "vitest";
import { historyInfo } from "./history-info";
import { setLanguage } from "./i18n";

afterEach(() => setLanguage("en"));

it("builds the artist caption from the stored count", () => {
    expect(historyInfo({ type: "artist", info: "12" })).toBe("12 albums");
});

it("uses the Russian plural form", () => {
    setLanguage("ru");
    expect(historyInfo({ type: "album", info: "12" })).toBe("12 треков");
});

it("leaves old and non-count captions as stored", () => {
    expect(historyInfo({ type: "album", info: "35 tracks" })).toBe("35 tracks");
    expect(historyInfo({ type: "track", info: "Artist" })).toBe("Artist");
});
