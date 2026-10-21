// @vitest-environment jsdom
import { act } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { history } from "../../wailsjs/go/models";
import { ClearFetchHistory, ClearFetchHistoryByType, DeleteFetchHistoryItem, GetFetchHistory } from "../../wailsjs/go/main/App";
import { toastWithSound as toast } from "@/lib/toast-with-sound";
import { HistoryPage } from "./HistoryPage";
import { TooltipProvider } from "./ui/tooltip";
import { mount, button, change } from "../test/render";

vi.mock("../../wailsjs/go/main/App", () => ({ GetFetchHistory: vi.fn(), DeleteFetchHistoryItem: vi.fn(), ClearFetchHistory: vi.fn(), ClearFetchHistoryByType: vi.fn() }));
vi.mock("@/lib/toast-with-sound", () => ({ toastWithSound: { success: vi.fn(), error: vi.fn() } }));

const entry = (id: string, type: string, name: string): history.FetchItem => ({ id, url: `https://open.spotify.com/${type}/${id}`, type, name, info: `${name} details`, image: "", data: `{"cached":"${id}"}`, timestamp: 1_700_000_000 });
const items = [entry("t1", "track", "Song One"), entry("a1", "album", "Album One")];
const onHistorySelect = vi.fn();
let view: Awaited<ReturnType<typeof mount>>;

beforeEach(async () => {
    vi.resetAllMocks();
    vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
    vi.mocked(GetFetchHistory).mockResolvedValue(items);
    view = await mount(<TooltipProvider><HistoryPage onHistorySelect={onHistorySelect}/></TooltipProvider>);
});
afterEach(async () => { await view.unmount(); vi.unstubAllGlobals(); });
const labelled = (label: string) => document.querySelector<HTMLElement>(`[aria-label="${label}"]`)!;

describe("HistoryPage", () => {
    it("lists the entries of the selected type only", async () => {
        expect(view.container.textContent).toContain("Song One");
        expect(view.container.textContent).not.toContain("Album One");
        await act(async () => button(view.container, "Albums").click());
        expect(view.container.textContent).toContain("Album One");
    });

    it("shows the type icon, not letters, when an entry has no image", () => {
        expect(view.container.querySelector("tbody tr svg")).not.toBeNull();
        expect(view.container.textContent).not.toContain("TR");
    });

    it("opens an entry by id and source link when its row is clicked", async () => {
        await act(async () => [...view.container.querySelectorAll("span")].find((span) => span.textContent === "Song One")!.click());
        expect(onHistorySelect).toHaveBeenCalledWith("t1", "https://open.spotify.com/track/t1");
    });

    it("has no Load button; the row itself opens the entry", async () => {
        expect(labelled("Load")).toBeNull();
        expect([...view.container.querySelectorAll("button")].some((item) => item.textContent?.trim() === "Load")).toBe(false);
    });

    it("opens a focused row on Enter, but not when Enter lands on the delete button", async () => {
        const row = view.container.querySelector<HTMLElement>("tbody tr")!;
        const press = (target: HTMLElement) => act(async () => { target.dispatchEvent(new KeyboardEvent("keydown", { key: "Enter", bubbles: true })); });

        expect(row.tabIndex).toBe(0);
        await press(labelled("Delete"));
        expect(onHistorySelect).not.toHaveBeenCalled();
        await press(row);
        expect(onHistorySelect).toHaveBeenCalledWith("t1", "https://open.spotify.com/track/t1");
    });

    it("deletes without opening the entry, then reloads", async () => {
        await act(async () => labelled("Delete").click());
        expect(DeleteFetchHistoryItem).toHaveBeenCalledWith("t1");
        expect(GetFetchHistory).toHaveBeenCalledTimes(2);
        expect(onHistorySelect).not.toHaveBeenCalled();
    });

    it("clears one type or everything after confirmation", async () => {
        await act(async () => button(view.container, "Clear Tracks").click());
        expect(ClearFetchHistoryByType).not.toHaveBeenCalled();
        await act(async () => button(document.body, "Clear History").click());
        expect(ClearFetchHistoryByType).toHaveBeenCalledWith("track");
        await act(async () => button(view.container, "Clear All").click());
        await act(async () => button(document.body, "Clear History").click());
        expect(ClearFetchHistory).toHaveBeenCalledTimes(1);
    });

    it("shows the string error Wails rejects with when history is unavailable", async () => {
        vi.mocked(GetFetchHistory).mockRejectedValue("history is unavailable");
        await view.unmount();
        view = await mount(<TooltipProvider><HistoryPage onHistorySelect={onHistorySelect}/></TooltipProvider>);
        expect(view.container.querySelector("[role=alert]")!.textContent).toContain("history is unavailable");
    });

    it("reports a failed delete instead of throwing", async () => {
        vi.mocked(DeleteFetchHistoryItem).mockRejectedValue("history is unavailable");
        await act(async () => labelled("Delete").click());
        expect(toast.error).toHaveBeenCalledWith("history is unavailable");
    });

    it("steps back a page when deleting the last row of the last page", async () => {
        const many = Array.from({ length: 51 }, (_, index) => entry(`t${index}`, "track", `Song ${String(index).padStart(2, "0")}`));
        many.forEach((item, index) => { item.timestamp = 1000 - index; });
        vi.mocked(GetFetchHistory).mockResolvedValue(many);
        await view.unmount();
        view = await mount(<TooltipProvider><HistoryPage onHistorySelect={onHistorySelect}/></TooltipProvider>);
        await act(async () => [...view.container.querySelectorAll("a")].find((a) => a.textContent === "2")!.click());
        expect(view.container.textContent).toContain("Song 50");
        vi.mocked(GetFetchHistory).mockResolvedValue(many.slice(0, 50));
        await act(async () => labelled("Delete").click());
        expect(view.container.textContent).toContain("Song 49");
        expect(view.container.textContent).not.toContain("No fetch history");
    });

    it("keeps Clear <Tab> enabled while a search matches nothing", async () => {
        const input = view.container.querySelector<HTMLInputElement>("input")!;
        await act(async () => change(input, "no such entry"));
        expect(view.container.textContent).toContain("No fetch history");
        expect(button(view.container, "Clear Tracks").disabled).toBe(false);
    });
});
