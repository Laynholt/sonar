// @vitest-environment jsdom
import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it, vi } from "vitest";
import type { TrackMetadata } from "@/types/api";
import { TrackList } from "./TrackList";

function makeTrack(n: number, over: Partial<TrackMetadata> = {}): TrackMetadata {
    return {
        service: "spotify",
        id: `track-${n}`,
        name: `Track ${n}`,
        artists: "Artist",
        album_name: "Album",
        duration_ms: 180_000,
        images: "",
        release_date: "2026",
        track_number: n,
        external_urls: `https://open.spotify.com/track/track-${n}`,
        ...over,
    };
}

function render(tracks: TrackMetadata[], props: { searchQuery?: string; sortBy?: string; currentPage?: number; itemsPerPage?: number } = {}): string {
    return renderToStaticMarkup(<TrackList
        tracks={tracks}
        searchQuery={props.searchQuery ?? ""}
        sortBy={props.sortBy ?? "default"}
        currentPage={props.currentPage ?? 1}
        itemsPerPage={props.itemsPerPage ?? 50}
        onPageChange={vi.fn()}
    />);
}

const titles = (markup: string) => [...markup.matchAll(/Track \d+/g)].map((m) => m[0]);

describe("TrackList", () => {
    it("renders the row fields", () => {
        const markup = render([makeTrack(1, { artists: "Some Artist", album_name: "Some Album", is_explicit: true, plays: "1234567" })]);

        expect(markup).toContain("Track 1");
        expect(markup).toContain("Some Artist");
        expect(markup).toContain("Some Album");
        expect(markup).toContain("3:00");
        expect(markup).toContain('title="Explicit"');
        expect(markup).toContain((1234567).toLocaleString());
    });

    it("filters by search query across title, artist and album", () => {
        const tracks = [makeTrack(1), makeTrack(2, { artists: "Needle" }), makeTrack(3, { album_name: "needle album" })];

        expect(titles(render(tracks, { searchQuery: "NEEDLE" }))).toEqual(["Track 2", "Track 3"]);
    });

    it("sorts by title descending and duration ascending", () => {
        const tracks = [makeTrack(1, { duration_ms: 300_000 }), makeTrack(2, { duration_ms: 100_000 }), makeTrack(3, { duration_ms: 200_000 })];

        expect(titles(render(tracks, { sortBy: "title-desc" }))).toEqual(["Track 3", "Track 2", "Track 1"]);
        expect(titles(render(tracks, { sortBy: "duration-asc" }))).toEqual(["Track 2", "Track 3", "Track 1"]);
    });

    it("paginates and only shows the requested page", () => {
        const tracks = [makeTrack(1), makeTrack(2), makeTrack(3)];

        const page2 = render(tracks, { currentPage: 2, itemsPerPage: 2 });

        expect(titles(page2)).toEqual(["Track 3"]);
        expect(page2).toContain('aria-label="Go to next page"');
        expect(render(tracks)).not.toContain('aria-label="Go to next page"');
    });

    it("links each row to its Spotify page", () => {
        expect(render([makeTrack(1)])).toContain('aria-label="Open in Spotify"');
    });

    it("shows no download controls", () => {
        const markup = render([makeTrack(1, { images: "https://img/1.jpg" })]);
        const doc = new DOMParser().parseFromString(markup, "text/html");

        expect(doc.querySelectorAll('[role="checkbox"]').length).toBe(0);
        const buttons = [...doc.querySelectorAll("button")].map((b) => `${b.getAttribute("aria-label") ?? ""} ${b.textContent ?? ""}`);
        expect(buttons.filter((name) => /download/i.test(name))).toEqual([]);
    });
});
