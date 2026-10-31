// @vitest-environment jsdom
import { act } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { useDownloads } from "@/hooks/useDownloads";
import { usePreview } from "@/hooks/usePreview";
import { chooseCandidate, skipReview, useLink, type DownloadItem } from "@/lib/downloads";
import { toastWithSound as toast } from "@/lib/toast-with-sound";
import { CandidatePicker } from "./CandidatePicker";
import { setSources, AMAZON, SPOTIFY } from "../test/sources";
import { button, change, deferred, mount } from "../test/render";

vi.mock("@/hooks/useDownloads", () => ({ useDownloads: vi.fn() }));
vi.mock("@/hooks/usePreview", () => ({ usePreview: vi.fn() }));
vi.mock("@/hooks/useSources", async () => (await import("../test/sources")).useSourcesMock);
vi.mock("@/lib/downloads", () => ({ chooseCandidate: vi.fn(), useLink: vi.fn(), skipReview: vi.fn() }));
vi.mock("@/lib/toast-with-sound", () => ({ toastWithSound: { success: vi.fn(), error: vi.fn() } }));

const cand = (id: string, score: number, extra: Record<string, unknown> = {}) =>
    ({ id, title: `Found ${id}`, artist: "Taylor", album: "Lover", release_date: "2019-08-23", image: "", duration_ms: 181000, explicit: false, score, recommended: false, ...extra });
const item = (id: string, status = "review", candidates: unknown[] = [cand("B1", 0.82, { recommended: true }), cand("B2", 0.714), cand("B3", 0.58)]) =>
    ({ id, queue_id: "album:x", status, candidates, review_service: "amazon", track: { service: "spotify", id: `S${id}`, title: `Song ${id}`, artist: "Artist", album: "Album", release_date: "2019-06-23", duration_ms: 178000, image: "" } }) as unknown as DownloadItem;

const play = vi.fn();
const onClose = vi.fn();
let view: Awaited<ReturnType<typeof mount>>;
const set = (items: DownloadItem[]) => vi.mocked(useDownloads).mockReturnValue({ items, byTrackId: new Map(), activeCount: 0, sites: [], held: false } as unknown as ReturnType<typeof useDownloads>);
const picker = (itemId: string) => <CandidatePicker itemId={itemId} onClose={onClose}/>;
const show = async (items: DownloadItem[], itemId = items[0].id) => { set(items); view = await mount(picker(itemId)); };
const text = () => document.body.textContent ?? "";
const buttons = (label: string) => [...document.body.querySelectorAll("button")].filter((b) => b.textContent?.trim() === label);
const click = (el: HTMLElement) => act(async () => el.click());
const plays = () => [...document.body.querySelectorAll<HTMLButtonElement>('button[aria-label="Play preview"]')];

beforeEach(() => {
    vi.resetAllMocks();
    setSources([SPOTIFY, AMAZON]);
    vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
    vi.mocked(usePreview).mockReturnValue({ playPreview: play, stopPreview: vi.fn(), loadingPreview: null, playingTrack: null });
    for (const f of [chooseCandidate, useLink, skipReview]) vi.mocked(f).mockResolvedValue(undefined);
});
afterEach(async () => { await view.unmount(); vi.unstubAllGlobals(); });

describe("CandidatePicker", () => {
    it("shows the Spotify track and one row per candidate with its percentage", async () => {
        await show([item("a")]);
        for (const part of ["Uncertain match", "From Spotify", "Song a", "Artist · Album · 2019", "2:58", "Found on Amazon", "Found B1", "Taylor · Lover · 2019", "3:01", "82%", "71%", "58%"]) expect(text()).toContain(part);
        expect(buttons("Choose")).toHaveLength(3);
    });

    it("badges only the recommended candidate and makes its button the primary one", async () => {
        await show([item("a")]);
        expect(text().match(/Recommended/g)).toHaveLength(1);
        const [first, second] = buttons("Choose");
        expect(first.className).toContain("bg-primary");
        expect(second.className).not.toContain("bg-primary");
    });

    it("shows at most four candidates", async () => {
        await show([item("a", "review", [cand("B1", 0.8), cand("B2", 0.7), cand("B3", 0.6), cand("B4", 0.5), cand("B5", 0.4)])]);
        expect(buttons("Choose")).toHaveLength(4);
        expect(text()).not.toContain("Found B5");
    });

    it("chooses a candidate and moves to the next review item", async () => {
        await show([item("a"), item("b")]);
        await click(buttons("Choose")[1]);
        expect(chooseCandidate).toHaveBeenCalledWith("a", "B2");
        expect(text()).toContain("Song b");
        expect(onClose).not.toHaveBeenCalled();
    });

    it("closes after the last review item is decided", async () => {
        await show([item("a")]);
        await click(buttons("Choose")[0]);
        expect(onClose).toHaveBeenCalledTimes(1);
    });

    it("skips the track and moves on", async () => {
        await show([item("a"), item("b")]);
        await click(button(document.body, "Skip track"));
        expect(skipReview).toHaveBeenCalledWith("a");
        expect(text()).toContain("Song b");
    });

    it("toasts a failed choice and stays on the item", async () => {
        vi.mocked(chooseCandidate).mockRejectedValue("nope");
        await show([item("a"), item("b")]);
        await click(buttons("Choose")[0]);
        expect(toast.error).toHaveBeenCalledWith("nope");
        expect(text()).toContain("Song a");
        expect(onClose).not.toHaveBeenCalled();
    });

    it("keeps the dialog mounted when the refresh after a choice lands before the call returns", async () => {
        const pending = deferred<void>();
        vi.mocked(chooseCandidate).mockReturnValue(pending.promise);
        await show([item("a"), item("b")]);
        const dialog = document.querySelector('[role=dialog]');
        await click(buttons("Choose")[0]);
        set([item("a", "queued"), item("b")]);
        await view.render(picker("a"));
        expect(document.querySelector('[role=dialog]')).toBe(dialog);
        expect(text()).toContain("Song b");
        await act(async () => pending.resolve());
        expect(document.querySelector('[role=dialog]')).toBe(dialog);
        expect(text()).toContain("Song b");
        expect(onClose).not.toHaveBeenCalled();
    });

    it("offers the link field when nothing was found and submits it", async () => {
        await show([item("a", "review", [])]);
        expect(text()).toContain("Nothing found");
        expect(buttons("Choose")).toHaveLength(0);
        change(document.body.querySelector("input")!, "https://music.amazon.com/albums/B0?trackAsin=B0X");
        await click(button(document.body, "Use link"));
        expect(useLink).toHaveBeenCalledWith("a", "https://music.amazon.com/albums/B0?trackAsin=B0X");
        expect(onClose).toHaveBeenCalled();
    });

    it("reveals the same field behind the paste-a-link link when candidates exist", async () => {
        await show([item("a")]);
        expect(document.body.querySelector("input")).toBeNull();
        await click(button(document.body, "None of these? Paste a link"));
        expect(document.body.querySelector("input")).not.toBeNull();
    });

    it("shows a rejected link inline and keeps the dialog open", async () => {
        vi.mocked(useLink).mockRejectedValue("paste a link to an Amazon Music track");
        await show([item("a", "review", [])]);
        change(document.body.querySelector("input")!, "nonsense");
        await click(button(document.body, "Use link"));
        expect(text()).toContain("paste a link to an Amazon Music track");
        expect(onClose).not.toHaveBeenCalled();
        expect(toast.error).not.toHaveBeenCalled();
    });

    it("disables the link button while empty or sending", async () => {
        const pending = deferred<void>();
        vi.mocked(useLink).mockReturnValue(pending.promise);
        await show([item("a", "review", [])]);
        expect(button(document.body, "Use link").disabled).toBe(true);
        change(document.body.querySelector("input")!, "https://music.amazon.com/tracks/B0X");
        expect(button(document.body, "Use link").disabled).toBe(false);
        await click(button(document.body, "Use link"));
        expect(button(document.body, "Use link").disabled).toBe(true);
        await act(async () => pending.resolve());
    });

    it("counts the review items in the footer and steps through them without deciding", async () => {
        await show([item("a"), item("x", "completed"), item("b"), item("c")]);
        expect(text()).toContain("1 of 3 awaiting a decision");
        await click(button(document.body, "next →"));
        expect(text()).toContain("Song b");
        expect(text()).toContain("2 of 3 awaiting a decision");
        await click(button(document.body, "next →"));
        await click(button(document.body, "next →"));
        expect(text()).toContain("Song a");
        expect(text()).toContain("1 of 3 awaiting a decision");
        expect(chooseCandidate).not.toHaveBeenCalled();
        expect(skipReview).not.toHaveBeenCalled();
    });

    it("hides next when it is the only review item", async () => {
        await show([item("a"), item("x", "completed")]);
        expect(text()).toContain("1 of 1 awaiting a decision");
        expect(buttons("next →")).toHaveLength(0);
    });

    it("names the services from the sources and offers a preview only where the source can preview", async () => {
        setSources([{ ...SPOTIFY, label: "Tidal" }, { ...AMAZON, label: "Qobuz", can: [] }]);
        await show([item("a")]);
        expect(text()).toContain("From Tidal");
        expect(text()).toContain("Found on Qobuz");
        expect(plays()).toHaveLength(1);
    });

    it("plays the Spotify preview and each candidate's preview", async () => {
        await show([item("a")]);
        expect(plays()).toHaveLength(4);
        await click(plays()[0]);
        expect(play).toHaveBeenLastCalledWith("spotify", "Sa", "Song a");
        await click(plays()[2]);
        expect(play).toHaveBeenLastCalledWith("amazon", "B2", "Found B2");
    });

    it("shows a spinner on the preview that is loading", async () => {
        vi.mocked(usePreview).mockReturnValue({ playPreview: play, stopPreview: vi.fn(), loadingPreview: "B1", playingTrack: null });
        await show([item("a")]);
        expect(document.body.querySelectorAll("[role=status]")).toHaveLength(1);
    });

    it("moves on when the open item stops being a review item", async () => {
        await show([item("a"), item("b")]);
        set([item("a", "queued"), item("b")]);
        await view.render(picker("a"));
        expect(text()).toContain("Song b");
        expect(onClose).not.toHaveBeenCalled();
        set([item("a", "queued"), item("b", "queued")]);
        await view.render(picker("a"));
        expect(onClose).toHaveBeenCalled();
    });
});
