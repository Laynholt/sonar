// @vitest-environment jsdom
import { afterEach, expect, it } from "vitest";
import { historyInfo, uniqueRecent } from "./history-info";
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

it("shows each recent card once, newest first, up to the limit", () => {
    const item = (id: string, type: string, name: string, image = "c.jpg") => ({ id, type, name, image });
    const items = [item("1", "artist", "Taylor Swift"), item("2", "album", "Showgirl"), item("3", "artist", "Taylor Swift"), item("4", "track", "Taylor Swift"), item("5", "album", "Other")];
    expect(uniqueRecent(items, 10).map((i) => i.id)).toEqual(["1", "2", "4", "5"]);
    expect(uniqueRecent(items, 2).map((i) => i.id)).toEqual(["1", "2"]);
});
