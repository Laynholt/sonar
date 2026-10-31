import { describe, expect, it } from "vitest";
import { move, parseConflict, parsePartial, textIn } from "./plugins";

describe("textIn", () => {
    it("picks the language, then English, then the first key, like Go's Text.In", () => {
        expect(textIn({ en: "Pause", ru: "Пауза" }, "ru")).toBe("Пауза");
        expect(textIn({ en: "Pause", ru: "Пауза" }, "de")).toBe("Pause");
        expect(textIn({ fr: "b", de: "a" }, "ru")).toBe("a");
        expect(textIn(null, "ru")).toBe("");
        expect(textIn(undefined, "en")).toBe("");
    });
});

describe("binding answers", () => {
    it("reads the ids of an import conflict", () => {
        expect(parseConflict("conflict:a,b")).toEqual(["a", "b"]);
        expect(parseConflict(new Error("conflict:a"))).toEqual(["a"]);
        expect(parseConflict("exists")).toBeNull();
    });

    it("reads what an import installed before it failed", () => {
        expect(parsePartial('installed:a,b:plugin "c": rename x: Access is denied.')).toEqual({ ids: ["a", "b"], error: 'plugin "c": rename x: Access is denied.' });
        expect(parsePartial("conflict:a")).toBeNull();
        expect(parsePartial("boom")).toBeNull();
    });
});

describe("move", () => {
    it("moves one item and leaves the list it got alone", () => {
        const list = ["a", "b", "c"];
        expect(move(list, 0, 2)).toEqual(["b", "c", "a"]);
        expect(move(list, 2, 1)).toEqual(["a", "c", "b"]);
        expect(list).toEqual(["a", "b", "c"]);
    });
});
