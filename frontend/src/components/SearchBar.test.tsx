// @vitest-environment jsdom
import { act } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { SearchBar } from "./SearchBar";
import { TooltipProvider } from "./ui/tooltip";
import { Search, SearchByType } from "../../wailsjs/go/main/App";
import { model } from "../../wailsjs/go/models";
import type { history } from "../../wailsjs/go/models";
import { mount, change, button, deferred } from "../test/render";
import { setSources, AMAZON, SPOTIFY } from "../test/sources";

vi.mock("../../wailsjs/go/main/App", () => ({ Search: vi.fn(), SearchByType: vi.fn() }));
vi.mock("@/hooks/useSources", async () => (await import("../test/sources")).useSourcesMock);
vi.mock("@/hooks/useTypingEffect", () => ({ useTypingEffect: (texts: string[]) => texts[0] }));

function result(name: string) {
    return new model.SearchResult({ id: name, name, type: "track", artists: "Artist", images: "", external_urls: "https://open.spotify.com/track/1", duration_ms: 1000, is_explicit: false, release_date: "", owner: "" });
}
function response(name: string, count = 1, albums = false) {
    return new model.SearchResponse({ tracks: Array.from({ length: count }, (_, index) => result(`${name} ${index}`)), albums: albums ? [result("Album")] : [], artists: [], playlists: [] });
}
const props = { url: "", loading: false, onUrlChange: vi.fn(), onFetch: vi.fn(), onFetchUrl: vi.fn(), history: [], onHistorySelect: vi.fn(), onHistoryRemove: vi.fn(), hasResult: false, searchMode: true, onSearchModeChange: vi.fn(), canGoBack: false, onBack: vi.fn() };
let view: Awaited<ReturnType<typeof mount>>;
beforeEach(async () => {
    vi.resetAllMocks();
    setSources([SPOTIFY, AMAZON]);
    vi.useFakeTimers();
    localStorage.clear();
    vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
    view = await mount(<TooltipProvider><SearchBar {...props}/></TooltipProvider>);
});
afterEach(async () => { await view.unmount(); vi.useRealTimers(); vi.unstubAllGlobals(); });
// React 19 passes console.error a different number of args per message, so scan them all.
const loggedSameKey = (spy: { mock: { calls: unknown[][] } }) => spy.mock.calls.some((args) => args.some((arg) => String(arg).includes("same key")));
async function query(value: string) {
    await act(async () => change(view.container.querySelector<HTMLInputElement>("#spotify-search")!, value));
}
async function start(value: string) {
    await query(value);
    await act(async () => vi.advanceTimersByTimeAsync(400));
}

describe("search request lifecycle", () => {
    it("shows waiting during debounce and empty only after a successful empty response", async () => {
        vi.mocked(Search).mockResolvedValue(response("", 0));
        await query("empty");
        expect(view.container.textContent).not.toContain("No results found");
        expect(view.container.textContent).toContain("Waiting");
        await act(async () => vi.advanceTimersByTimeAsync(400));
        expect(view.container.textContent).toContain("No results found");
    });

    it.each(["resolve", "reject"])("ignores an old %s as soon as the query changes, before the next debounce", async (outcome) => {
        const old = deferred<model.SearchResponse>();
        vi.mocked(Search).mockReturnValueOnce(old.promise).mockResolvedValueOnce(response("Fresh"));
        await start("old");
        await act(async () => {
            change(view.container.querySelector<HTMLInputElement>("#spotify-search")!, "new");
            if (outcome === "resolve") old.resolve(response("Stale"));
            else old.reject(new Error("stale network error"));
            await Promise.resolve();
        });
        expect(view.container.textContent).not.toContain("Stale");
        expect(view.container.textContent).not.toContain("stale network error");
        expect(view.container.textContent).toContain("Waiting");
        expect(localStorage.getItem("sonar_recent_searches") || "").not.toContain("old");
        await act(async () => vi.advanceTimersByTimeAsync(400));
        expect(view.container.textContent).toContain("Fresh");
    });

    it.each(["clear", "mode"])("ignores a response after %s", async (action) => {
        const old = deferred<model.SearchResponse>();
        vi.mocked(Search).mockReturnValue(old.promise);
        await start("old");
        if (action === "clear") await query("");
        else await view.render(<TooltipProvider><SearchBar {...props} searchMode={false}/></TooltipProvider>);
        await act(async () => old.resolve(response("Stale")));
        if (action === "mode") await view.render(<TooltipProvider><SearchBar {...props}/></TooltipProvider>);
        expect(view.container.textContent).not.toContain("Stale");
        expect(localStorage.getItem("sonar_recent_searches") || "").not.toContain("old");
    });

    it("shows a service error separately from empty results and retries through the button", async () => {
        vi.mocked(Search).mockRejectedValueOnce(new Error("service offline")).mockResolvedValueOnce(response("Recovered"));
        await start("retry");
        expect(view.container.textContent).toContain("service offline");
        expect(view.container.textContent).not.toContain("No results found");
        await act(async () => button(view.container, "Retry search").click());
        await act(async () => vi.advanceTimersByTimeAsync(400));
        expect(view.container.textContent).toContain("Recovered");
    });

    it.each(["resolve", "reject"])("preserves a newer successful search when the older request later %ss", async (outcome) => {
        const old = deferred<model.SearchResponse>();
        vi.mocked(Search).mockReturnValueOnce(old.promise).mockResolvedValueOnce(response("Latest"));
        await start("old");
        await start("new");
        await act(async () => {
            if (outcome === "resolve") old.resolve(response("Stale"));
            else old.reject(new Error("stale failure"));
        });
        expect(view.container.textContent).toContain("Latest");
        expect(view.container.textContent).not.toContain("Stale");
        expect(view.container.textContent).not.toContain("stale failure");
    });

    it("retains results on pagination error and retries the same page through the button", async () => {
        vi.mocked(Search).mockResolvedValue(response("Initial", 50));
        vi.mocked(SearchByType).mockRejectedValueOnce(new Error("page offline")).mockResolvedValueOnce([result("Recovered page")]);
        await start("first");
        await act(async () => button(view.container, "Load More").click());
        expect(view.container.textContent).toContain("Initial");
        expect(view.container.textContent).toContain("page offline");
        await act(async () => button(view.container, "Retry loading more").click());
        expect(view.container.textContent).toContain("Recovered page");
        expect(view.container.textContent).not.toContain("page offline");
        expect(SearchByType).toHaveBeenLastCalledWith("spotify", "first", "track", 50, 50);
    });

    it("ignores a pagination rejection from the previous tab", async () => {
        const page = deferred<model.SearchResult[]>();
        vi.mocked(Search).mockResolvedValue(response("Initial", 50, true));
        vi.mocked(SearchByType).mockReturnValue(page.promise);
        await start("first");
        await act(async () => button(view.container, "Load More").click());
        await act(async () => button(view.container, "Albums (1)").click());
        await act(async () => page.reject(new Error("stale page error")));
        expect(view.container.textContent).toContain("Album");
        expect(view.container.textContent).not.toContain("stale page error");
        await act(async () => button(view.container, "Tracks (50)").click());
        expect(button(view.container, "Load More").disabled).toBe(false);
    });

    it.each(["query", "tab"])("discards pagination after a %s change", async (action) => {
        const page = deferred<model.SearchResult[]>();
        vi.mocked(Search).mockResolvedValue(response("Initial", 50, true));
        vi.mocked(SearchByType).mockReturnValue(page.promise);
        await start("first");
        await act(async () => button(view.container, "Load More").click());
        if (action === "query") {
            vi.mocked(Search).mockResolvedValue(response("Replacement"));
            await start("second");
        } else await act(async () => button(view.container, "Albums (1)").click());
        await act(async () => page.resolve([result("Stale page")]));
        if (action === "tab") {
            expect(view.container.textContent).toContain("Tracks (50)");
            await act(async () => button(view.container, "Tracks (50)").click());
        }
        expect(view.container.textContent).not.toContain("Stale page");
        expect(view.container.textContent).toContain(action === "query" ? "Replacement" : "Initial");
    });
});

describe("search rows", () => {
    it("hides a zero duration instead of showing 0:00", async () => {
        const hit = result("Hit");
        hit.duration_ms = 0;
        vi.mocked(Search).mockResolvedValue(new model.SearchResponse({ tracks: [hit], albums: [], artists: [], playlists: [] }));
        await start("hit");
        expect(view.container.textContent).toContain("Hit");
        expect(view.container.textContent).not.toContain("0:00");
    });
});

describe("modes and recent fetches", () => {
    const item = (id: string, name: string): history.FetchItem => ({ id, url: `https://open.spotify.com/track/${id}`, type: "track", name, info: "Some Artist", image: "", data: "{}", timestamp: 1 });
    const placeholder = () => view.container.querySelector("input")!.placeholder;

    it("uses the link prompt in link mode and an example query in search mode", async () => {
        expect(placeholder()).toBe("Golden");
        await view.render(<TooltipProvider><SearchBar {...props} searchMode={false}/></TooltipProvider>);
        expect(placeholder()).toBe("Paste a Spotify / Amazon Music link");
    });

    it.each([
        ["https://music.amazon.com/albums/B00A3SGWYC"],
        ["https://open.spotify.com/track/1"],
        ["https://example.com/x"],
    ])("sends %s to the backend on Enter in link mode, which decides if a source takes it", async (link) => {
        const onFetch = vi.fn();
        await view.render(<TooltipProvider><SearchBar {...props} searchMode={false} url={link} onFetch={onFetch}/></TooltipProvider>);
        await act(async () => view.container.querySelector("#spotify-url")!.dispatchEvent(new KeyboardEvent("keydown", { key: "Enter", bubbles: true })));
        expect(onFetch).toHaveBeenCalledTimes(1);
        expect(document.body.querySelector('[role="dialog"]')).toBeNull();
    });

    it("lists recent fetches in link mode and routes select and remove", async () => {
        const onHistorySelect = vi.fn();
        const onHistoryRemove = vi.fn();
        const recent = [item("a", "First song")];
        await view.render(<TooltipProvider><SearchBar {...props} searchMode={false} history={recent} onHistorySelect={onHistorySelect} onHistoryRemove={onHistoryRemove}/></TooltipProvider>);
        expect(view.container.textContent).toContain("Recent Fetch");
        expect(view.container.textContent).toContain("Some Artist");
        await act(async () => view.container.querySelector<HTMLElement>("[title='First song']")!.click());
        expect(onHistorySelect).toHaveBeenCalledWith(recent[0]);
        await act(async () => view.container.querySelector<HTMLElement>("[aria-label=\"Remove First song from recent fetches\"]")!.click());
        expect(onHistoryRemove).toHaveBeenCalledWith("a");
        expect(onHistorySelect).toHaveBeenCalledTimes(1);
    });

    it("hides recent fetches once a result is showing", async () => {
        await view.render(<TooltipProvider><SearchBar {...props} searchMode={false} hasResult history={[item("a", "First song")]}/></TooltipProvider>);
        expect(view.container.textContent).not.toContain("Recent Fetch");
    });

    it("shows the message of a string rejection, as Wails delivers it", async () => {
        vi.mocked(Search).mockRejectedValueOnce("spotify is down");
        await start("boom");
        expect(view.container.textContent).toContain("Search failed: spotify is down");
    });

    it("opens a clicked result by its link and leaves search mode", async () => {
        vi.mocked(Search).mockResolvedValue(response("Hit"));
        await start("hit");
        await act(async () => view.container.querySelector<HTMLElement>("button.bg-card")!.click());
        expect(props.onSearchModeChange).toHaveBeenCalledWith(false);
        expect(props.onFetchUrl).toHaveBeenCalledWith("https://open.spotify.com/track/1");
    });

    it("appends only new items when the next page overlaps the previous one", async () => {
        vi.mocked(Search).mockResolvedValue(response("Initial", 50));
        const errors = vi.spyOn(console, "error").mockImplementation(() => {});
        // Page 2 repeats two items from page 1 and repeats a new item within itself.
        vi.mocked(SearchByType).mockResolvedValue([result("Initial 48"), result("Initial 49"), result("Fresh"), result("Fresh")]);
        await start("first");
        await act(async () => button(view.container, "Load More").click());
        const rows = [...view.container.querySelectorAll("button.bg-card")].map((row) => row.querySelector("p")!.textContent);
        expect(rows).toHaveLength(51);
        expect(new Set(rows).size).toBe(51);
        expect(rows.at(-1)).toBe("Fresh");
        expect(button(view.container, "Tracks (51)")).toBeTruthy();
        expect(loggedSameKey(errors)).toBe(false);
        errors.mockRestore();
    });

    it("renders a duplicated id in the first page once", async () => {
        const errors = vi.spyOn(console, "error").mockImplementation(() => {});
        vi.mocked(Search).mockResolvedValue(new model.SearchResponse({ tracks: [result("Dup"), result("Dup"), result("Other")], albums: [], artists: [], playlists: [] }));
        await start("first");
        const rows = [...view.container.querySelectorAll("button.bg-card")].map((row) => row.querySelector("p")!.textContent);
        expect(rows).toEqual(["Dup", "Other"]);
        expect(loggedSameKey(errors)).toBe(false);
        errors.mockRestore();
    });

    it("advances Load More by the raw API offset after an all-duplicate page", async () => {
        vi.mocked(Search).mockResolvedValue(response("Initial", 50));
        vi.mocked(SearchByType).mockResolvedValue(Array.from({ length: 50 }, (_, index) => result(`Initial ${index}`)));
        await start("first");
        await act(async () => button(view.container, "Load More").click());
        await act(async () => button(view.container, "Load More").click());
        expect(vi.mocked(SearchByType).mock.calls.map((call) => call[4])).toEqual([50, 100]);
    });

    it("keeps Load More when a full page was all duplicates", async () => {
        vi.mocked(Search).mockResolvedValue(response("Initial", 50));
        vi.mocked(SearchByType).mockResolvedValue(Array.from({ length: 50 }, (_, index) => result(`Initial ${index}`)));
        await start("first");
        await act(async () => button(view.container, "Load More").click());
        expect(view.container.textContent).toContain("Tracks (50)");
        expect(button(view.container, "Load More").disabled).toBe(false);
    });
});

describe("search service switch", () => {
    const pick = (label: string) => act(async () => button(view.container, label).click());

    it("searches Amazon Music through Search with amazon, without load more", async () => {
        vi.mocked(Search).mockResolvedValue(response("Amazon hit", 50));
        await pick("Amazon Music");
        await start("olivia");
        expect(Search).toHaveBeenCalledWith("amazon", "olivia", 50);
        expect(vi.mocked(Search).mock.calls.some(([service]) => service === "spotify")).toBe(false);
        expect(view.container.textContent).toContain("Amazon hit 0");
        expect(view.container.textContent).not.toContain("Load More");
        expect(view.container.querySelector("#spotify-search")!.getAttribute("aria-label")).toBe("Search Amazon Music");
    });

    it("remembers the choice after a remount", async () => {
        await pick("Amazon Music");
        expect(localStorage.getItem("sonar_search_service")).toBe("amazon");
        await view.unmount();
        view = await mount(<TooltipProvider><SearchBar {...props}/></TooltipProvider>);
        expect(button(view.container, "Amazon Music").getAttribute("aria-checked")).toBe("true");
        expect(button(view.container, "Spotify").getAttribute("aria-checked")).toBe("false");
    });

    const remount = async () => {
        await view.unmount();
        view = await mount(<TooltipProvider><SearchBar {...props}/></TooltipProvider>);
    };
    const radios = () => [...view.container.querySelectorAll('[role="radio"]')].map((item) => item.textContent);

    it("lists the sources that can search, with the source's icon when it has one and a generic one when not", async () => {
        const noSearch = { ...AMAZON, service: "noisy", label: "Noisy", can: ["preview"] };
        setSources([{ ...SPOTIFY, icon: "data:image/png;base64,AAAA" }, AMAZON, noSearch]);
        await remount();
        expect(radios()).toEqual(["Spotify", "Amazon Music"]);
        const [first, second] = view.container.querySelectorAll('[role="radio"]');
        expect(first.querySelector("img")!.getAttribute("src")).toBe("data:image/png;base64,AAAA");
        expect(second.querySelector("img")).toBeNull();
        expect(second.querySelector("svg")).not.toBeNull();
    });

    it("hides the switch when only one source can search", async () => {
        setSources([SPOTIFY]);
        await remount();
        expect(radios()).toEqual([]);
        vi.mocked(Search).mockResolvedValue(response("Solo"));
        await start("one");
        expect(Search).toHaveBeenCalledWith("spotify", "one", 50);
    });

    it("offers load more only for a source that can search by type", async () => {
        vi.mocked(Search).mockResolvedValue(response("Hit", 50));
        vi.mocked(SearchByType).mockResolvedValue([]);
        await start("first");
        await act(async () => button(view.container, "Load More").click());
        expect(SearchByType).toHaveBeenCalledWith("spotify", "first", "track", 50, 50);
        setSources([SPOTIFY, { ...AMAZON, can: ["search", "searchByType"] }]);
        await pick("Amazon Music");
        await act(async () => vi.advanceTimersByTimeAsync(400));
        await act(async () => button(view.container, "Load More").click());
        expect(SearchByType).toHaveBeenLastCalledWith("amazon", "first", "track", 50, 50);
    });

    it("falls back to the first source when the remembered one is gone", async () => {
        localStorage.setItem("sonar_search_service", "gone");
        await remount();
        expect(button(view.container, "Spotify").getAttribute("aria-checked")).toBe("true");
        localStorage.setItem("sonar_search_service", "amazon");
        setSources([SPOTIFY]);
        await remount();
        vi.mocked(Search).mockResolvedValue(response("Fallback"));
        await start("x");
        expect(Search).toHaveBeenCalledWith("spotify", "x", 50);
    });

    it("keeps Amazon results when a late Spotify response arrives after switching", async () => {
        const late = deferred<model.SearchResponse>();
        vi.mocked(Search).mockImplementation((service) => service === "spotify" ? late.promise : Promise.resolve(response("Amazon")));
        await start("olivia");
        await pick("Amazon Music");
        await act(async () => vi.advanceTimersByTimeAsync(400));
        expect(view.container.textContent).toContain("Amazon 0");
        await act(async () => late.resolve(response("Late spotify")));
        expect(view.container.textContent).toContain("Amazon 0");
        expect(view.container.textContent).not.toContain("Late spotify");
    });
});

describe("back button", () => {
    const back = () => view.container.querySelector<HTMLButtonElement>('button[aria-label="Back"]')!;

    it("is first in the row and disabled when there is nothing to go back to", () => {
        expect(back().disabled).toBe(true);
        expect(view.container.querySelector("button")).toBe(back());
    });

    it("calls onBack when enabled", async () => {
        await view.render(<TooltipProvider><SearchBar {...props} canGoBack/></TooltipProvider>);
        expect(back().disabled).toBe(false);
        await act(async () => back().click());
        expect(props.onBack).toHaveBeenCalledTimes(1);
    });
});
