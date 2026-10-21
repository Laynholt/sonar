import { describe, expect, it } from "vitest";
import { emptyNav, navigate, type NavState } from "./navigation";
import type { LoadedMetadata } from "@/types/api";

const entry = (name: string): LoadedMetadata => ({
    data: { service: "spotify", playlist_info: { tracks: { total: 0 }, followers: { total: 0 }, owner: { display_name: "", name, images: "" } }, track_list: [] },
    sourceUrl: `https://open.spotify.com/playlist/${name}`,
});
const view = (page: number) => ({ searchQuery: "q", sortBy: "default", page });
const a = entry("a"), b = entry("b"), c = entry("c"), d = entry("d");

describe("navigate", () => {
    it("open sets current and empties back", () => {
        const state: NavState = { current: a, back: [b] };
        expect(navigate(state, { type: "open", entry: c })).toEqual({ current: c, back: [] });
    });

    it("push puts the old current, with its view, on back", () => {
        const next = navigate({ current: a, back: [] }, { type: "push", entry: b, view: view(2) });
        expect(next).toEqual({ current: b, back: [{ ...a, view: view(2) }] });
    });

    it("push from nothing leaves back empty", () => {
        expect(navigate(emptyNav, { type: "push", entry: a, view: view(1) })).toEqual({ current: a, back: [] });
    });

    it("back pops into current, view included, and is a no-op on empty", () => {
        const state = navigate({ current: a, back: [] }, { type: "push", entry: b, view: view(2) });
        expect(navigate(state, { type: "back" })).toEqual({ current: { ...a, view: view(2) }, back: [] });
        expect(navigate({ current: a, back: [] }, { type: "back" })).toEqual({ current: a, back: [] });
    });

    it("close empties both", () => {
        expect(navigate({ current: a, back: [b] }, { type: "close" })).toEqual(emptyNav);
    });

    it("open after a three-step chain empties back", () => {
        let state = navigate(emptyNav, { type: "open", entry: a });
        for (const next of [b, c, d]) state = navigate(state, { type: "push", entry: next, view: view(1) });
        expect(state.back).toHaveLength(3);
        expect(navigate(state, { type: "open", entry: a }).back).toEqual([]);
    });
});
